package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
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
	LogFile  string

	Daemon bool
}

var (
	runDeployment = false
	logFile = os.Stdout
)

func openLogFile(path string) *os.File {
	if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0774); err != nil {
		log.Println("Couldn't open log file")
		log.Println(err)
		return os.Stdout
	} else {
		return file
	}
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
		LogFile:  "",
		Daemon:   false,
	}

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		// Set correct working directory
		if wd, err := filepath.Abs(filepath.Dir(os.Args[0])); err != nil {
			log.Println(err)
			os.Exit(-1)
		} else if err := os.Chdir(wd); err != nil {
			log.Println(err)
			os.Exit(-1)
		} else {
			os.Args[0] = "./" + filepath.Base(os.Args[0])
			if err := godotenv.Load(); err != nil {
				log.Println("Error loading environment file")
				log.Println(err)
			}
		}
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
		logFile = openLogFile(val)
	}

	return cfg
}

func main() {
	cfg := LoadBlogConfig()
	server := &http.Server{
		Addr: cfg.Addr,
	}

	// Handle automatic deployment and daemon
	HandleDaemon(cfg, server)

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
	if err := server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile); err != http.ErrServerClosed {
		log.Println(err)
		log.Println("Failed opening up HTTPS server, defaulting to normal HTTP")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Println(err)
			return
		}
	}

	// Run deployment script if nessesary
	if !runDeployment {
		// Close the old logfile
		logFile.Close()
		return
	}

	log.Println("Handling deployment...")

	// Pull latest code from git
	if err := exec.Command("git", "pull", "origin").Run(); err != nil {
		log.Println(err)
	} else {
		log.Println("Successfully pulled form origin")
	}

	// Build new server code
	if err := exec.Command("go", "build", "./server").Run(); err != nil {
		log.Println(err)
	} else {
		log.Println("Successfully built server code")
	}

	// Close the old logfile
	logFile.Close()

	// Now execute the new blog executable
	err = syscall.Exec("./blog", []string{"-d"}, os.Environ())
	
	// Re-open log file
	if err != nil && cfg.LogFile != "" {
		logFile = openLogFile(cfg.LogFile)
		if logFile == os.Stdout {
			os.Exit(-1)
		}
		
		log.SetOutput(logFile)
		log.Println(err)
		log.Println("Couldn't start the next blog instance")
		logFile.Close()
		os.Exit(-1)
	}
}

func HandleDaemon(cfg *BlogConfig, server *http.Server) {
	// Daemonize process
	if os.Getenv("DAEMONIZED") == "" && cfg.Daemon {
		// Close log file
		if logFile != os.Stdout {
			logFile.Close()
		}

		// Configure background process
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), "DAEMONIZED=1")
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
	log.SetOutput(logFile)

	// Create PID file
	if cfg.PIDFile != "" {
		data := []byte(strconv.FormatInt(int64(syscall.Getpid()), 10))
		err := os.WriteFile(cfg.PIDFile, data, 0777)
		if err != nil {
			log.Println(err)
			log.Println("Couldn't create the pid file!")
		}
	}

	deploySignal := make(chan struct{})

	go func() {
		// Wait for deployment signal or terminate gracefully
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		select {
		case <-deploySignal:
		case <-interrupt:
		}

		// Close the HTTP Server
		log.Println("Shutting down server...")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Println(err)
		}
	}()
	
	if os.Getenv("DAEMONIZED") != "" {
		// Re-deploy if we are a dameon
		http.HandleFunc("POST /admin/deploy", func(w http.ResponseWriter, r *http.Request) {
			go func() {
				runDeployment = true
				deploySignal <- struct{}{}
			}()
		})
	}
}
