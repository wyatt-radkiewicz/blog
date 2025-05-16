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

	"github.com/joho/godotenv"
)

type BlogConfig struct {
	PostDir  string
	Password string
	Title    string
	CertFile string
	KeyFile  string
	Addr     string
	PIDFile  string
	LogFile  *os.File

	Daemon bool
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
		LogFile:  os.Stdout,
		Daemon:   false,
	}

	// Set correct working directory
	if wd, err := filepath.Abs(filepath.Dir(os.Args[0])); err != nil {
		log.Println(err)
		os.Exit(-1)
	} else if err := os.Chdir(wd); err != nil {
		log.Println(err)
		os.Exit(-1)
	}

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Error loading environment file")
		log.Println(err)
	}

	// Load command line parameters
	daemon := flag.Bool("d", false, "Whether to daemonize the program")
	flag.Parse()
	cfg.Daemon = *daemon

	// Get environment variables
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
	if val, ok := os.LookupEnv("BLOG_PIDFILE"); ok {
		cfg.PIDFile = val
	}
	if val, ok := os.LookupEnv("BLOG_LOGFILE"); ok {
		var err error
		cfg.LogFile, err = os.Create(val)
		if err != nil {
			log.Println("Couldn't open log file")
			log.Println(err)
			cfg.LogFile = os.Stdout
		}
	}

	return cfg
}

func main() {
	cfg := LoadBlogConfig()

	// Handle automatic deployment and daemon
	HandleDaemon(cfg)

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
	if err := http.ListenAndServeTLS(cfg.Addr, cfg.CertFile, cfg.KeyFile, nil); err != nil {
		log.Println(err)
		log.Println("Failed opening up HTTPS server, defaulting to normal HTTP")
		if err := http.ListenAndServe(cfg.Addr, nil); err != nil {
			log.Println(err)
			return
		}
	}
}

func HandleDaemon(cfg *BlogConfig) {
	// Daemonize process
	if os.Getenv("DAEMONIZED") == "" && cfg.Daemon {
		// Close log file
		if cfg.LogFile != os.Stdout {
			cfg.LogFile.Close()
		}

		// Get working directory and basename of executable
		wd, err := os.Getwd()
		if err != nil {
			log.Println(err)
			log.Println("Couldn't find working directory")
			os.Exit(-1)
		}
		basename := filepath.Base(os.Args[0])

		// Configure background process
		cmd := exec.Command("./" + basename, os.Args[1:]...)
		cmd.Env = append(os.Environ(), "DAEMONIZED=1")
		cmd.Dir = wd
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
		}
		cmd.Stdout = nil
		cmd.Stdin = nil
		cmd.Stderr = nil

		// Start background process
		if err := cmd.Start(); err != nil {
			log.Println(err)
			log.Println("Couldn't daemonize process!!")
			os.Exit(-1)
		} else {
			os.Exit(0)
		}
	}

	// Set correct logfile
	log.SetOutput(cfg.LogFile)

	// Create PID file
	if cfg.PIDFile != "" {
		data := []byte(strconv.FormatInt(int64(syscall.Getpid()), 10))
		err := os.WriteFile(cfg.PIDFile, data, 0777)
		if err != nil {
			log.Println(err)
			log.Println("Couldn't create the pid file!")
		}
	}

	if os.Getenv("DAEMONIZED") != "" {
		// Re-deploy if we are a dameon
		http.HandleFunc("POST /admin/deploy", func(w http.ResponseWriter, r *http.Request) {
			sh, err := exec.LookPath("sh")
			if err != nil {
				log.Println("Can't find shell to run deployment script on")
				return
			}

			script := "git pull origin; rm blog; go build ./server && exec ./blog -d"
			syscall.Exec(sh, []string{"sh", "-c", script}, os.Environ())
		})
	}
}
