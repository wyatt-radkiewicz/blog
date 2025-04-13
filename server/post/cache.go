package post

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"golang.org/x/exp/slices"
)

type Cache struct {
	dir     string
	watcher *fsnotify.Watcher
	posts   map[string]*Post
	mu		sync.RWMutex
}

func LoadCache(dir string) (*Cache, error) {
	c := Cache{
		dir:   dir,
		posts: make(map[string]*Post),
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

		p, err := loadPost(filepath.Join(dir, ent.Name()))
		if err != nil {
			return nil, err
		}
		c.posts[ent.Name()] = &p
	}

	// Watch for changes of markdown files
	go func() {
		select {
		case event, ok := <-c.watcher.Events:
			if !ok {
				return
			}

			c.mu.Lock()
			switch event.Op {
			case fsnotify.Remove, fsnotify.Write:
				delete(c.posts, event.Name)
			}

			switch event.Op {
			case fsnotify.Create, fsnotify.Write:
				if p, err := loadPost(filepath.Join(dir, event.Name)); err == nil {
					c.posts[event.Name] = &p
				}
			}
			c.mu.Unlock()
		case event, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
			log.Println(event)
		}
	}()
	if err = c.watcher.Add(dir); err != nil {
		return nil, err
	} else {
		return &c, nil
	}
}

func (c *Cache) CloseCache() {
	c.watcher.Close()
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
	c.mu.RLock()
outer:
	for _, p := range c.posts {
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
	c.mu.RUnlock()

	// Sort the results
	slices.SortFunc(results, func(a, b queryResult) int {
		if a.rank > b.rank {
			return 1
		} else if a.rank < b.rank {
			return -1
		} else {
			if a.post.Created.After(b.post.Created) {
				return 1
			} else if a.post.Created.Before(b.post.Created) {
				return -1
			} else {
				return 0
			}
		}
	})

	posts := make([]Post, len(results))
	for i, r := range results {
		posts[i] = r.post
	}
	return posts
}
