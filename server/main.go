package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	_ "github.com/joho/godotenv/autoload"
)

type BlogConfig struct {
	PostDir  string
	Password string
	Title    string
	CertFile string
	KeyFile  string
}

func LoadBlogConfig() *BlogConfig {
	cfg := &BlogConfig{
		PostDir:  "posts",
		Password: "admin",
		Title:    "Eklipsed's Blog",
		CertFile: "server.crt",
		KeyFile:  "server.key",
	}

	if val, ok := os.LookupEnv("BLOG_POST_DIR"); ok {
		cfg.PostDir = val
	}
	if val, ok := os.LookupEnv("BLOG_PASSWORD"); ok {
		cfg.Password = val
	}
	if val, ok := os.LookupEnv("BLOG_TITLE"); ok {
		cfg.Title = val
	}
	if val, ok := os.LookupEnv("BLOG_CERT_FILE"); ok {
		cfg.CertFile = val
	}
	if val, ok := os.LookupEnv("BLOG_KEY_FILE"); ok {
		cfg.KeyFile = val
	}
	return cfg
}

func main() {
	cfg := LoadBlogConfig()
	ps, err := NewPostStats(cfg)
	if err != nil {
		log.Println(err)
		return
	}

	// Handle uploads, admin page, and the upload page
	HandleAdmin(ps)

	// Handle viewing posts/main pages
	HandlePosts(ps)

	// Handle viewing posts by tags
	HandleTags(ps)

	// Serve attachments
	http.HandleFunc("/attachments/{postid}/{file}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(cfg.PostDir, r.PathValue("postid"), r.PathValue("file")))
	})

	// Serve static content in the ./static directory
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})

	// Run the server
	if err := http.ListenAndServeTLS("", cfg.CertFile, cfg.KeyFile, nil); err != nil {
		log.Println(err)
		log.Println("Failed opening up HTTPS server, defaulting to normal HTTP")
		if err := http.ListenAndServe("", nil); err != nil {
			log.Println(err)
			return
		}
	}
}
