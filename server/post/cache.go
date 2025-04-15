package post

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"golang.org/x/exp/slices"
)

type Cache struct {
	dir     string
	watcher *fsnotify.Watcher
	Posts   map[string]*Post
	Tags    map[string]int
	Mu      sync.RWMutex
}

func LoadCache(dir string) (*Cache, error) {
	c := Cache{
		dir:   dir,
		Posts: make(map[string]*Post),
		Tags:  make(map[string]int),
	}

	// Read the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	c.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Load in each entry
	for _, ent := range entries {
		if ent.IsDir() || filepath.Ext(ent.Name()) != ".md" {
			continue
		}

		if err := c.cache(ent.Name()); err != nil {
			return nil, err
		}
	}

	// Watch for changes of markdown files
	go func() {
		for {
			select {
			case event, ok := <-c.watcher.Events:
				if !ok {
					return
				}

				c.Mu.Lock()
				switch {
				case event.Has(fsnotify.Remove), event.Has(fsnotify.Write):
					c.uncache(filepath.Base(event.Name))
				}

				switch {
				case event.Has(fsnotify.Create), event.Has(fsnotify.Write):
					if err := c.cache(filepath.Base(event.Name)); err != nil {
						fmt.Println(err)
					}
				}
				c.Mu.Unlock()
			case event, ok := <-c.watcher.Errors:
				if !ok {
					return
				}
				log.Println(event)
			}
		}
	}()
	if err = c.watcher.AddWith(dir); err != nil {
		return nil, err
	} else {
		return &c, nil
	}
}

func (c *Cache) Close() error {
	return c.watcher.Close()
}

func (c *Cache) cache(name string) error {
	p, err := loadPost(filepath.Join(c.dir, name))
	if err != nil {
		return err
	}
	c.Posts[name] = &p
	for tag := range p.Tags {
		c.Tags[tag] += 1
	}
	return nil
}

func (c *Cache) uncache(name string) {
	p, ok := c.Posts[name]
	if !ok {
		return
	}
	for tag, _ := range p.Tags {
		if rc, ok := c.Tags[tag]; ok {
			if rc == 1 {
				delete(c.Tags, tag)
			} else {
				c.Tags[tag] = rc - 1
			}
		}
	}
	delete(c.Posts, name)
}

type QueryOptions struct {
	// Tags needed in the query
	Tags []string

	// Load posts after this time
	After time.Time

	// Load posts before this time
	Before time.Time

	// Fuzzy searching for title parameter
	Search string
}

func (c *Cache) Query(opts QueryOptions) []Post {
	type queryResult struct {
		post Post
		rank int
	}

	results := make([]queryResult, 0)

	// Start search in after and to before
	c.Mu.RLock()
outer:
	for _, p := range c.Posts {
		// Check for needed tags
		for _, needed := range opts.Tags {
			if _, ok := p.Tags[needed]; !ok {
				continue outer
			}
		}

		// Check date range
		if p.Created.After(opts.Before) || p.Created.Before(opts.After) {
			continue outer
		}

		// Add it to the posts list
		res := queryResult{
			post: *p,
			rank: 5,
		}
		if opts.Search != "" {
			res.rank = fuzzy.RankMatch(opts.Search, p.Title)
		}
		results = append(results, res)
	}
	c.Mu.RUnlock()

	// Sort the results
	slices.SortFunc(results, func(a, b queryResult) int {
		if a.rank > b.rank {
			return 1
		} else if a.rank < b.rank {
			return -1
		} else {
			if a.post.Created.Before(b.post.Created) {
				return 1
			} else if a.post.Created.After(b.post.Created) {
				return -1
			} else {
				return strings.Compare(a.post.Title, b.post.Title)
			}
		}
	})

	posts := make([]Post, len(results))
	for i, r := range results {
		posts[i] = r.post
	}
	return posts
}
