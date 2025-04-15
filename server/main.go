package main

import (
	"log"
	"net/http"

	"eklipsed/blog/config"
	"eklipsed/blog/post"
	"eklipsed/blog/routes"
	"eklipsed/blog/template"
)

func main() {
	cfg := config.Load("config.json")
	c, err := post.LoadCache("posts")
	if err != nil {
		log.Fatal("Can't load cache", err)
	}
	defer c.Close()
	
	t, err := template.NewCache("views")
	if err != nil {
		log.Fatal("Can't load template cache", err)
	}
	defer t.Close()
	
	ms := &routes.MainServer{
		Config: &cfg,
		Cache: c,
		Tmpl: t,
	}
	fs := http.FileServer(http.Dir("static"))

	http.Handle("/posts/{$}", ms)
	http.Handle("/{$}", ms)
	http.Handle("/", fs)
	http.ListenAndServe(":3000", nil)
}
