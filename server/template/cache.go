package template

import (
	"fmt"
	"html/template"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Template struct {
	files []string
	Tmpl  *template.Template
}

type Cache struct {
	dir     string
	tmpl    map[string]*Template
	files   map[string][]*Template
	watcher *fsnotify.Watcher
	mu      sync.RWMutex
}

func NewCache(dir string) (*Cache, error) {
	var err error
	var c Cache

	if c.watcher, err = fsnotify.NewWatcher(); err != nil {
		return nil, err
	}
	c.files = make(map[string][]*Template)
	c.tmpl = make(map[string]*Template)
	c.dir = dir

	go func() {
		for {
			select {
			case event, ok := <-c.watcher.Events:
				if !ok {
					return
				}
				switch {
				case event.Has(fsnotify.Create), event.Has(fsnotify.Write):
					c.mu.Lock()
					for _, t := range c.files[event.Name] {
						var err error
						t.Tmpl, err = template.ParseFiles(t.files...)
						if err != nil {
							fmt.Println(err)
						}
					}
					c.mu.Unlock()
				}
			case err, ok := <-c.watcher.Errors:
				if !ok {
					return
				}
				fmt.Println(err)
			}
		}
	}()
	
	if err := c.watcher.Add(dir); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Cache) Close() error {
	return c.watcher.Close()
}

func (c *Cache) Add(files ...string) *Template {
	tmpl := &Template{
		files: files,
		Tmpl:  nil,
	}
	if t, err := template.ParseFiles(files...); err == nil {
		tmpl.Tmpl = t
	} else {
		fmt.Println(err)
	}

	c.mu.Lock()
	for _, file := range tmpl.files {
		if _, ok := c.files[file]; !ok {
			c.files[file] = make([]*Template, 0)
		}
		c.files[file] = append(c.files[file], tmpl)
	}
	c.tmpl[strings.Join(files, ":")] = tmpl
	defer c.mu.Unlock()

	return tmpl
}

func (c *Cache) Get(files ...string) *template.Template {
	c.mu.RLock()
	t, ok := c.tmpl[strings.Join(files, ":")]
	if ok {
		defer c.mu.RUnlock()
		return t.Tmpl
	} else {
		c.mu.RUnlock()
		return c.Add(files...).Tmpl
	}
}
