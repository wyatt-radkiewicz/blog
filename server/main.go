package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	_ "github.com/joho/godotenv/autoload"
)

type BlogConfig struct {
	PostDir  string
	Password string
	Title    string
	CertFile string
	KeyFile  string
	Addr     string
	PIDFile  string
}

func LoadBlogConfig() *BlogConfig {
	cfg := &BlogConfig{
		PostDir:  "posts",
		Password: "admin",
		Title:    "Eklipsed's Blog",
		CertFile: "server.crt",
		KeyFile:  "server.key",
		Addr:     ":3000",
		PIDFile:  "",
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
	if val, ok := os.LookupEnv("BLOG_ADDR"); ok {
		cfg.Addr = val
	}
	
	pidfile := flag.String("p", "", "Where to write the pid file")
	flag.Parse()
	cfg.PIDFile = *pidfile
	
	return cfg
}

func main() {
	cfg := LoadBlogConfig()
	
	if cfg.PIDFile != "" {
		data := []byte(strconv.FormatInt(int64(syscall.Getpid()), 10))
		err := os.WriteFile(cfg.PIDFile, data, 0777)
		if err != nil {
			log.Println(err)
			log.Println("Couldn't create the pid file!")
		}
	}
	
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

	// Handle automatic deployment
	HandleDeploy(cfg)

	// Run the server
	if err := http.ListenAndServeTLS(cfg.Addr, cfg.CertFile, cfg.KeyFile, nil); err != nil {
		log.Println(err)
		log.Println("Failed opening up HTTPS server, defaulting to normal HTTP")
		if err := http.ListenAndServe(cfg.Addr, nil); err != nil {
			log.Println(err)
			return
		}
	}
}

func HandleDeploy(cfg *BlogConfig) {
	http.HandleFunc("POST /admin/deploy", func(w http.ResponseWriter, r *http.Request) {
		sh, err := exec.LookPath("sh")
		if err != nil {
			log.Println("Can't find shell to run deployment script on")
			return
		}

		script := "git pull origin; rm blog; go build ./server && ./blog"
		syscall.Exec(sh, []string{"sh", "-c", script}, os.Environ())
	})
}
