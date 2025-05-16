package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Session [32]byte

func NewSession() Session {
	var session [32]byte
	rand.Read(session[:])
	return session
}

func (s Session) GetString() string {
	return base64.RawURLEncoding.EncodeToString(s[:])
}

func (s Session) CheckString(str string) bool {
	if buf, err := base64.RawURLEncoding.DecodeString(str); err == nil {
		return bytes.Equal(s[:], buf)
	} else {
		return false
	}
}

func (s Session) GetCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "Session",
		Value:    s.GetString(),
		Expires:  time.Now().Add(time.Hour * 24),
		SameSite: http.SameSiteLaxMode,
	}
}

// Allows new passwords and may update the session
func (s *Session) CheckAndAccept(w http.ResponseWriter, r *http.Request, pass string) bool {
	if err := r.ParseForm(); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return false
	}

	if r.Form.Has("Password") {
		if r.Form.Get("Password") != pass {
			return false
		}

		*s = NewSession()
		http.SetCookie(w, s.GetCookie())
		return true
	}

	if c, err := r.Cookie("Session"); err != http.ErrNoCookie {
		return s.CheckString(c.Value)
	} else {
		return false
	}
}

// Generates a postID, error should never be returned but if the stars align it might
func GeneratePostID(dir string) (PostID, error) {
	for i := 0; i < 64; i++ {
		var buf [6]byte
		rand.Read(buf[:])
		id := base64.RawURLEncoding.EncodeToString(buf[:])
		if _, err := os.Stat(filepath.Join(dir, id)); err != nil && os.IsNotExist(err) {
			return PostID(id), nil
		}
	}

	return "", fmt.Errorf("Couldn't generate post ID")
}

func HandleAdmin(ps *PostStats) {
	type Info struct {
		Info *PostInfo
		Date string
		ID string
	}

	// Only 1 admin account allowed, session is just saved as string and randomly generated uuid
	session := NewSession()
	upload := func(w http.ResponseWriter, r *http.Request) {
		if !session.CheckAndAccept(w, r, ps.Cfg.Password) {
			return
		}

		if err := r.ParseMultipartForm((1 << 20) * 64); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Create the directory for the new post
		id, err := GeneratePostID(ps.Cfg.PostDir)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		postdir := filepath.Join(ps.Cfg.PostDir, string(id))
		if err := os.Mkdir(postdir, 0755); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Copy all files in
		if headers, ok := r.MultipartForm.File["post"]; !ok {
			log.Println("Error when parsing multipart form")
			w.WriteHeader(http.StatusBadRequest)
			return
		} else {
			for _, h := range headers {
				// Try opening the file for reading
				data, err := h.Open()
				if err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				// Create the destination file and copy the contents
				file, err := os.Create(filepath.Join(postdir, h.Filename))
				if err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if _, err := io.Copy(file, data); err != nil {
					log.Println(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				data.Close()
				file.Close()
			}
		}

		// Validate/lint the resulting post directory
		if ok, err := ValidatePost(postdir); err != nil {
			log.Println(err)
			if err := os.RemoveAll(postdir); err != nil {
				log.Println(err)
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		} else if !ok {
			log.Println("Post is invalid")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := ps.Add(id); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if info, err := LoadPostInfo(filepath.Join(ps.Cfg.PostDir, string(id))); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
		} else if r.PathValue("postid") != "" {
			// If this is is an update response return the new row
			tmpl, err := template.ParseFiles("views/admin-post.html")
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// Now that we have the new ID, lets try to get the old ID from postid and swap the two
			postid := PostID(r.PathValue("postid"))
			ps.Remove(postid, true)
			ps.Remove(id, false)
			os.Rename(filepath.Join(ps.Cfg.PostDir, string(id)), filepath.Join(ps.Cfg.PostDir, string(postid)))
			ps.Add(postid)

			if err := tmpl.ExecuteTemplate(w, "post", Info{
				Info: &info,
				Date: FormatDate(info.Date),
				ID: string(postid),
			}); err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
			}
		} else {
			// Do normal upload response
			fmt.Fprintln(w, info.Title)
			fmt.Fprintln(w, id)
		}
	}

	// Handle uploading to the filesystem and updating posts
	http.HandleFunc("POST /admin/upload", upload)
	http.HandleFunc("POST /admin/update/{postid}", upload)

	// Handle deleting posts
	http.HandleFunc("DELETE /admin/delete/{postid}", func(w http.ResponseWriter, r *http.Request) {
		if !session.CheckAndAccept(w, r, ps.Cfg.Password) {
			return
		}

		if _, err := ps.Remove(PostID(r.PathValue("postid")), true); err != nil {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	// Handle downloading posts
	http.HandleFunc("GET /admin/download/{postid}", func(w http.ResponseWriter, r *http.Request) {
		if !session.CheckAndAccept(w, r, ps.Cfg.Password) {
			return
		}

		// Construct the zip file first
		buffer := bytes.NewBuffer(nil)
		writer := zip.NewWriter(buffer)
		if err := writer.AddFS(os.DirFS(filepath.Join(ps.Cfg.PostDir, r.PathValue("postid")))); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := writer.Close(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reader := bytes.NewReader(buffer.Bytes())
		http.ServeContent(w, r, fmt.Sprintf("%s.zip", r.PathValue("postid")), time.Now(), reader)
	})

	getposts := func(w http.ResponseWriter, r *http.Request) {
		if !session.CheckAndAccept(w, r, ps.Cfg.Password) {
			http.ServeFile(w, r, "views/admin-pass.html")
			return
		}

		tmpl, err := template.ParseFiles(
			"views/admin.html",
			"views/admin-posts.html",
			"views/admin-post.html",
			"views/nav.html",
		)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Get every post ordered by date
		posts := make([]Info, 0)
		ps.Lock.RLock()
		for _, id := range ps.ByDate {
			info := ps.Posts[id]
			posts = append(posts, Info{
				Date: FormatDate(info.Date),
				Info: &info,
				ID: string(id),
			})
		}
		ps.Lock.RUnlock()

		term := strings.TrimSpace(r.Form.Get("Search"))
		if term != "" {
			posts = make([]Info, 0)
			for _, id := range ps.SearchAndRank(term) {
				info := ps.Posts[id]
				posts = append(posts, Info{
					Date: FormatDate(info.Date),
					Info: &info,
					ID: string(id),
				})
			}

			if len(posts) == 0 {
				fmt.Fprint(w, "<h3><em>No posts found...</em></h3>")
				return
			}
		}

		var exec string
		if r.URL.Path == "/admin/posts" || r.Form.Has("Search") {
			exec = "posts"
		} else {
			exec = "base"
		}

		// Return the template
		if err := tmpl.ExecuteTemplate(w, exec, struct {
			Title        string
			SearchTarget string
			Posts        []Info
		}{
			Title:        "Eklipsed Blog Admin Page",
			SearchTarget: "#posts-list",
			Posts:        posts,
		}); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	// Handle getting the main admin page and returning for htmx the posts list
	http.HandleFunc("/admin", getposts)
	http.HandleFunc("GET /admin/posts", getposts)
}

// This removes uneeded files and makes sure that the post is correct.
// If the post is completely invalid, it will return false
func ValidatePost(dir string) (bool, error) {
	// Just try to load the post first
	post, err := LoadPost(dir)
	if err != nil {
		return false, err
	}

	// Now remove everthing in the directory that is not a part of the post
	if ents, err := os.ReadDir(dir); err != nil {
		return false, err
	} else {
		for _, ent := range ents {
			name := ent.Name()
			if name == "post.md" || name == "post.toml" {
				continue
			}
			if _, ok := post.Attachments[name]; ok {
				continue
			}
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				log.Println(err)
				return false, nil
			}
		}
	}

	return true, nil
}
