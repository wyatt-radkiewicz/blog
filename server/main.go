package main

import (
	"eklipsed/blog/post"
	"fmt"
	"log"
	"time"
)

func main() {
	c, err := post.LoadCache("posts")
	if err != nil {
		log.Fatal("Can't load cache")
	}
	defer c.CloseCache()

	fmt.Println(c.Query(post.QueryOptions{
		Tags:   []string{},
		After:  time.Unix(0, 0),
		Before: time.Now(),
		Search: "",
	}))
}
