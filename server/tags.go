package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
)

// Handle tags page
func HandleTags(ps *PostStats) {
	type Info struct {
		Title string
		Date  string
		ID    string
	}

	type Tag struct {
		Tag   string
		ID    string
		Num   int
		Posts []Info
	}

	// Function to write the data for a tag
	gettag := func(name string) (Tag, error) {
		ps.Lock.RLock()
		defer ps.Lock.RUnlock()

		postids, ok := ps.ByTag[name]
		if !ok {
			return Tag{}, fmt.Errorf("Expected tag to have a entry in ps.Tags")
		}

		tag := Tag{
			Tag:   name,
			ID:    string(ps.TagDB.GetTagID(name)),
			Num:   len(postids),
			Posts: nil,
		}

		tag.Posts = make([]Info, tag.Num)
		for i, post := range postids {
			info := ps.Posts[post]
			tag.Posts[i] = Info{
				Title: info.Title,
				Date:  FormatDate(info.Date),
				ID:    string(post),
			}
		}
		return tag, nil
	}

	// Get html for specific tag
	http.HandleFunc("GET /tags/{tag}", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		tmpl, err := template.ParseFiles("views/tag.html")
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		
		var tag Tag
		for name, id := range ps.TagDB {
			if string(id) != r.PathValue("tag") {
				continue
			}
			
			tag, err = gettag(name)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			break
		}
		if !r.Form.Has("showPosts") {
			tag.Posts = nil
		}

		if err := tmpl.ExecuteTemplate(w, "tag", tag); err != nil {
			log.Println(err)
		}
	})

	// Get the tags page
	http.HandleFunc("/tags", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles(
			"views/base.html",
			"views/nav.html",
			"views/tags.html",
			"views/tag.html",
		)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		r.ParseForm()
		tags := make([]Tag, 0)
		expand := r.Form.Get("expand")
		search := strings.TrimSpace(r.Form.Get("Search"))

		ps.Lock.RLock()
		for name, id := range ps.TagDB {
			tag, err := gettag(name)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if string(id) != expand {
				tag.Posts = nil
			}
			if search == "" || fuzzy.RankMatchNormalizedFold(search, tag.Tag) != -1 {
				tags = append(tags, tag)
			}
		}
		ps.Lock.RUnlock()

		slices.SortFunc(tags, func(a Tag, b Tag) int {
			if a.Num > b.Num {
				return -1
			} else if a.Num < b.Num {
				return 1
			} else {
				return strings.Compare(a.Tag, b.Tag)
			}
		})

		var exec string
		if r.Form.Has("Search") {
			exec = "main"
			if len(tags) == 0 {
				fmt.Fprint(w, "<h3><em>No tags found...</em></h3>")
				return
			}
		} else {
			exec = "base"
		}

		if err := tmpl.ExecuteTemplate(w, exec, struct {
			Title        string
			SearchTarget string
			Tags         []Tag
		}{
			Title:        "Sort by Tags",
			SearchTarget: "main",
			Tags:         tags,
		}); err != nil {
			log.Println(err)
		}
	})
}
