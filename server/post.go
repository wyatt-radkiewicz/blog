package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"image"
	_ "image/gif"
	_ "image/png"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/lithammer/fuzzysearch/fuzzy"
)

type TagID string
type PostID string

// Post metadata
type PostInfo struct {
	Title string
	Date  time.Time
	Tags  []string
}

// Actual post data
type Post struct {
	Info        PostInfo
	Id          PostID
	Document    template.HTML
	Attachments map[string]struct{}
}

type TagDB map[string]TagID

type PostStats struct {
	Posts  map[PostID]PostInfo
	ByDate []PostID
	ByTag  map[string][]PostID
	TagDB  TagDB
	Cfg    *BlogConfig
	Lock   sync.RWMutex // Mutex for thread safe access
}

func LoadTagDB(dir string) (TagDB, error) {
	var db TagDB

	// See if the database exists, if so open it
	if data, err := os.ReadFile(filepath.Join(dir, "tags.toml")); err != nil &&
		!os.IsNotExist(err) {
		// Some error in opening the file, it does exist it seems
		log.Println(err)
		return nil, err
	} else if os.IsNotExist(err) {
		db = make(map[string]TagID)
	} else if err := toml.Unmarshal(data, &db); err == nil {
		return db, nil
	}

	// Okay so either the database is corrupted or there is no tagdb so lets just create one
	var entries []os.DirEntry
	var err error
	if entries, err = os.ReadDir(dir); err != nil {
		log.Println(err)
		return nil, err
	}

	for _, ent := range entries {
		// Get the post info, and then register the tag
		if ent.Type() != fs.ModeDir {
			continue
		}
		if info, err := LoadPostInfo(filepath.Join(dir, ent.Name())); err != nil {
			log.Println(err)
			return nil, err
		} else {
			for _, tag := range info.Tags {
				db.GetTagID(tag)
			}
		}
	}

	if err := db.Save(dir); err != nil {
		log.Println(err)
		return nil, err
	} else {
		return db, nil
	}
}

func (db *TagDB) GetTagID(name string) TagID {
	if id, ok := (*db)[name]; ok {
		return id
	}

	// Okay create a new id
	for {
		var buf [6]byte
		rand.Read(buf[:])
		newid := TagID(base64.RawURLEncoding.EncodeToString(buf[:]))

		unique := true
		for _, id := range *db {
			if id == newid {
				unique = false
				break
			}
		}

		if unique {
			(*db)[name] = newid
			return newid
		}
	}
}

func (db *TagDB) Save(dir string) error {
	if data, err := toml.Marshal(db); err != nil {
		return err
	} else {
		return os.WriteFile(filepath.Join(dir, "tags.toml"), data, 0664)
	}
}

// Formats time for everything across the site
func FormatDate(t time.Time) string {
	return fmt.Sprintf("%d %s %d", t.Day(), t.Month().String(), t.Year())
}

// Returns an ordered list of post UUID's
func (ps *PostStats) SearchAndRank(term string) []PostID {
	type result struct {
		info PostInfo
		id   PostID
		rank int
	}

	results := make([]result, 0)
	ps.Lock.RLock()
	for id, post := range ps.Posts {
		rank := fuzzy.RankMatchNormalizedFold(term, post.Title)
		if rank == -1 {
			continue
		}
		results = append(results, result{
			info: post,
			id:   id,
			rank: rank,
		})
	}
	ps.Lock.RUnlock()

	slices.SortFunc(results, func(a, b result) int {
		if a.rank > b.rank {
			return -1
		} else if a.rank < b.rank {
			return 1
		} else {
			if a.info.Date.After(b.info.Date) {
				return -1
			} else if a.info.Date.Before(b.info.Date) {
				return 1
			} else {
				return 0
			}
		}
	})

	posts := make([]PostID, len(results))
	for i, res := range results {
		posts[i] = res.id
	}

	return posts
}

// Read all directories from a post directory and create a new post stats
func NewPostStats(cfg *BlogConfig) (*PostStats, error) {
	entries, err := os.ReadDir(cfg.PostDir)
	if err != nil {
		return nil, err
	}

	ps := &PostStats{
		Posts:  make(map[PostID]PostInfo, 0),
		ByDate: make([]PostID, 0),
		ByTag:  make(map[string][]PostID),
		TagDB:  nil,
		Cfg:    cfg,
	}
	if ps.TagDB, err = LoadTagDB(cfg.PostDir); err != nil {
		return ps, err
	}
	for _, entry := range entries {
		if entry.Type() != fs.ModeDir {
			continue
		}
		if err := ps.Add(PostID(entry.Name())); err != nil {
			return ps, err
		}
	}
	return ps, nil
}

// Remove a post
// This removes the directory (if remdir is set), and the listing
// If the post doesn't exist it will return false
func (ps *PostStats) Remove(id PostID, remdir bool) (bool, error) {
	ps.Lock.Lock()
	defer ps.Lock.Unlock()

	// Remove it from the date ordering and posts map
	if _, ok := ps.Posts[id]; !ok {
		return false, nil
	}
	delete(ps.Posts, id)
	i := slices.Index(ps.ByDate, id)
	ps.ByDate = slices.Delete(ps.ByDate, i, i+1)

	// Remove tag refrences
	deadtags := make([]string, 0)
	for tag, posts := range ps.ByTag {
		if i := slices.Index(posts, id); i != -1 {
			ps.ByTag[tag] = slices.Delete(ps.ByTag[tag], i, i+1)
		}
		if len(ps.ByTag[tag]) == 0 {
			deadtags = append(deadtags, tag)
		}
	}
	for _, tag := range deadtags {
		delete(ps.ByTag, tag)
		delete(ps.TagDB, tag)
	}
	ps.TagDB.Save(ps.Cfg.PostDir)

	// Remove it from the posts directory
	if !remdir {
		return true, nil
	} else if err := os.RemoveAll(filepath.Join(ps.Cfg.PostDir, string(id))); err != nil {
		return true, err
	} else {
		return true, nil
	}
}

// Adds information from a uuid in the posts directory
func (ps *PostStats) Add(id PostID) error {
	ps.Lock.Lock()
	defer ps.Lock.Unlock()

	// Delete previous entry if its there
	if _, ok := ps.Posts[id]; ok {
		ps.Remove(id, false)
	}

	// Try to get the info of the newly added post
	info, err := LoadPostInfo(filepath.Join(ps.Cfg.PostDir, string(id)))
	if err != nil {
		return err
	}

	// Add ordered date info
	i := sort.Search(len(ps.ByDate), func(i int) bool {
		return ps.Posts[ps.ByDate[i]].Date.Before(info.Date)
	})
	if i == -1 {
		i = 0
	}
	ps.ByDate = slices.Insert(ps.ByDate, i, id)

	// Add the tags, hashes and normal info
	ps.Posts[id] = info
	for _, tag := range info.Tags {
		ps.ByTag[tag] = append(ps.ByTag[tag], id)
		ps.TagDB.GetTagID(tag)
	}
	ps.TagDB.Save(ps.Cfg.PostDir)

	return nil
}

// Converts path to point to URL of the attachment and adds it to the post's attachemnt list
func convertPath(postdir string, post *Post, path string) string {
	// Add it to the list of attachments
	if _, err := os.Stat(filepath.Join(postdir, path)); err == nil {
		post.Attachments[path] = struct{}{}

		// Make sure the destination points to the url of the attachment
		path = filepath.Join("/attachments", string(post.Id), path)
	}

	return path
}

// Post images can also contain captions
type PostImage struct {
	File    string
	Alt     string
	Caption string
}

func LoadPostInfo(dir string) (PostInfo, error) {
	var info PostInfo
	path := filepath.Join(dir, "post.toml")
	if data, err := os.ReadFile(path); err != nil {
		log.Println(err)
		return info, err
	} else if err = toml.Unmarshal(data, &info); err != nil {
		log.Println(err)
		return info, err
	} else {
		return info, nil
	}
}

// Load a post from a directory, if it can't it will return an error
func LoadPost(dir string) (post Post, err error) {
	// Get the UUID from the name of the dir
	post.Id = PostID(filepath.Base(dir))
	post.Attachments = make(map[string]struct{})

	// Parse the metadata file
	if post.Info, err = LoadPostInfo(dir); err != nil {
		return
	}

	// Parse the post contents
	var md ast.Node
	var data []byte
	if data, err = os.ReadFile(filepath.Join(dir, "post.md")); err != nil {
		log.Println(err)
		return
	} else {
		p := parser.NewWithExtensions(parser.CommonExtensions | parser.Footnotes)
		md = markdown.Parse(data, p)

		// What the next header id should be (gets incremented every time)
		hdrid := 1
		ast.WalkFunc(md, func(node ast.Node, entering bool) ast.WalkStatus {
			// Pixelate certain images
			if img, ok := node.(*ast.Image); ok {
				oldpath := string(img.Destination)
				newpath := convertPath(dir, &post, oldpath)
				img.Destination = []byte(newpath)

				if file, err := os.Open(filepath.Join(dir, oldpath)); err == nil {
					pixelFormats := map[string]bool{
						"png": true,
						"gif": true,
					}

					mdata, format, err := image.DecodeConfig(file)
					if err != nil || !pixelFormats[format] {
						return ast.GoToNext
					}
					if mdata.Width >= 640 || mdata.Height >= 480 {
						return ast.GoToNext
					}

					// Add the class to pixelate the image
					img.Attribute = &ast.Attribute{
						Classes: [][]byte{[]byte("lowres")},
					}
				}
			}

			// Put metadata in headings
			if hdr, ok := node.(*ast.Heading); ok {
				hdr.Attribute = &ast.Attribute{
					ID:      fmt.Appendf(nil, "%s-hdr%d", post.Id, hdrid),
					Classes: [][]byte{[]byte("copy-header")},
					Attrs: map[string][]byte{
						"post": []byte(post.Id),
					},
				}
				hdrid += 1
			}

			return ast.GoToNext
		})
	}

	// Render out the HTML
	r := html.NewRenderer(html.RendererOptions{
		Flags: html.CommonFlags | html.HrefTargetBlank,
		RenderNodeHook: func(w io.Writer, node ast.Node, entering bool) (ast.WalkStatus, bool) {
			// Handle different image rendering
			if img, ok := node.(*ast.Image); ok {
				if entering {
					fmt.Fprint(w, "<div class=\"centered-image\">")
					fmt.Fprintf(w, "<img src=\"%s\" ", string(img.Destination))
					if img.Attribute != nil && len(img.Classes) > 0 {
						fmt.Fprint(w, "class=\"")
						for _, class := range img.Classes {
							fmt.Fprintf(w, "%s ", string(class))
						}
						fmt.Fprint(w, "\" ")
					}
					fmt.Fprint(w, "alt=\"")
				} else {
					fmt.Fprint(w, "\" /></div>")
				}
				return ast.GoToNext, true
			}

			// Render extra metadata for headers
			if hdr, ok := node.(*ast.Heading); ok {
				if entering {
					fmt.Fprintf(w, "<h%d><span id=\"%s\" class=\"%s\" post=\"%s\">",
						hdr.Level,
						string(hdr.ID),
						string(hdr.Classes[0]),
						string(hdr.Attrs["post"]))
				} else {
					fmt.Fprintf(w, "</span></h%d>", hdr.Level)
				}
				return ast.GoToNext, true
			}

			return ast.GoToNext, false
		},
	})
	post.Document = template.HTML(markdown.Render(md, r))
	return
}

// Handle main user pages
func HandlePosts(ps *PostStats) {
	type ServedPost struct {
		Post
		Expand     bool
		ShowButton bool
	}

	// Called with empty search term on the page
	defaultposts := func(r *http.Request, loadfrom, maxposts int) ([]ServedPost, error) {
		switch r.URL.Path {
		case "/about":
			if abouts, ok := ps.ByTag["About"]; !ok && len(abouts) == 0 {
				return nil, fmt.Errorf("About page not found!")
			} else if post, err := LoadPost(filepath.Join(ps.Cfg.PostDir, string(abouts[0]))); err != nil {
				return nil, fmt.Errorf("About page not found!")
			} else if loadfrom > 0 {
				return []ServedPost{}, nil
			} else {
				return []ServedPost{ServedPost{Post: post, Expand: true}}, nil
			}

		case "/home", "/":
			posts := make([]ServedPost, 0)
			for _, id := range ps.ByDate {
				if post, err := LoadPost(filepath.Join(ps.Cfg.PostDir, string(id))); err != nil {
					return nil, err
				} else {
					posts = append(posts, ServedPost{Post: post, Expand: true, ShowButton: true})
				}
			}
			return posts[min(loadfrom, len(posts)):min(loadfrom+maxposts, len(posts))], nil

		default:
			if post, err := LoadPost(filepath.Join(ps.Cfg.PostDir, r.PathValue("postid"))); err != nil {
				return nil, err
			} else if loadfrom > 0 {
				return []ServedPost{}, nil
			} else {
				return []ServedPost{ServedPost{Post: post, Expand: true}}, nil
			}
		}
	}

	// Called when the search term on the page is not empty
	// Search through all posts and fuzzy search the title and rank them
	searchposts := func(term string, loadfrom, maxposts int) ([]ServedPost, error) {
		posts := make([]ServedPost, 0)
		for _, id := range ps.SearchAndRank(term) {
			if post, err := LoadPost(filepath.Join(ps.Cfg.PostDir, string(id))); err != nil {
				return nil, err
			} else {
				posts = append(posts, ServedPost{Post: post, Expand: false, ShowButton: true})
			}
		}
		return posts[min(loadfrom, len(posts)):min(loadfrom+maxposts, len(posts))], nil
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.New("base").Funcs(map[string]any{
			"formatTime": FormatDate,
			"getID": func(name string) string {
				return string(ps.TagDB.GetTagID(name))
			},
		}).ParseFiles("views/base.html", "views/nav.html", "views/posts.html", "views/post.html")
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Template to execute
		exec := "main"
		if err := r.ParseForm(); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Shortcut to only load a single post from htmx expand/close thing
		if r.Form.Has("Expand") || r.Form.Has("Close") {
			var post ServedPost
			if p, err := LoadPost(filepath.Join(ps.Cfg.PostDir, r.PathValue("postid"))); err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			} else {
				post = ServedPost{
					Post:       p,
					Expand:     r.Form.Has("Expand"),
					ShowButton: true,
				}
			}

			if err := tmpl.ExecuteTemplate(w, "post", post); err != nil {
				log.Println(err)
			}
			return
		}

		// Try to see what posts to load
		loadfrom := 0
		maxposts := 3
		if i, err := strconv.ParseInt(r.Form.Get("LoadFrom"), 10, 32); err == nil {
			loadfrom = int(i)
		}

		// Try to do the posts search first
		var posts []ServedPost
		term := ""
		if r.Form.Has("Search") {
			term = strings.TrimSpace(r.Form.Get("Search"))

			if term != "" {
				posts, err = searchposts(term, loadfrom, maxposts)
			} else {
				posts, err = nil, nil
			}

			if loadfrom == 0 && posts != nil && len(posts) == 0 {
				fmt.Fprint(w, "<h3><em>No posts found...</em></h3>")
				return
			}
		} else if loadfrom == 0 {
			exec = "base"
		}

		// Get default posts
		if posts == nil {
			posts, err = defaultposts(r, loadfrom, maxposts)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}

		// If we were doing infinite scroll and there is nothing left, don't return anything
		if len(posts) == 0 && loadfrom != 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Construct url for next access
		nexturl := r.URL
		nexturl.RawQuery = fmt.Sprintf("LoadFrom=%d", loadfrom+maxposts)
		if term != "" {
			nexturl.RawQuery += fmt.Sprintf("&Search=%s", url.QueryEscape(term))
		}

		title := "Eklipsed's Blog"
		if s, ok := os.LookupEnv("BLOG_TITLE"); ok {
			title = s
		}
		if err := tmpl.ExecuteTemplate(w, exec, struct {
			Title        string
			SearchTarget string
			Posts        []ServedPost
			LoadPostsURL string
		}{
			Title:        title,
			SearchTarget: "main",
			Posts:        posts,
			LoadPostsURL: nexturl.String(),
		}); err != nil {
			log.Println(err)
			return
		}
	}
	http.HandleFunc("/post/{postid}", handler)
	http.HandleFunc("/about", handler)
	http.HandleFunc("/home", handler)
	http.HandleFunc("/{$}", handler)
}
