package routes

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"eklipsed/blog/config"
	"eklipsed/blog/post"
	"eklipsed/blog/template"
)

type MainServer struct {
	Cache  *post.Cache
	Config *config.Config
	Tmpl   *template.Cache
}

func (ms *MainServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := post.QueryOptions{
		Search: q.Get("Search"),
		Tags:   nil,
		After:  time.Unix(0, 0),
		Before: time.Now(),
	}
	if q.Has("Tags") {
		opts.Tags = strings.Split(q.Get("Tags"), ",")
	}
	posts := ms.Cache.Query(opts)

	type Info struct {
		Config *config.Config
		Posts  []post.Post
		Tags   map[string]int
	}

	ms.Cache.Mu.RLock()
	info := Info{
		Config: ms.Config,
		Posts:  posts,
		Tags:   ms.Cache.Tags,
	}
	data := bytes.NewBuffer(nil)
	ms.Tmpl.Get("views/base.html", "views/main.html", "views/posts.html").Execute(data, info)
	ms.Cache.Mu.RUnlock()
	w.Write(data.Bytes())
}
