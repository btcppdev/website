package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	texttemplate "text/template"
	"time"

	"btcpp-web/external/buffer"
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/external/tokens"
	youtubepkg "btcpp-web/external/youtube"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/handlers"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/boltstore"
	"github.com/alexedwards/scs/v2"
	bolt "go.etcd.io/bbolt"
)

var app config.AppContext

func loadConfig() *types.EnvConfig {
	config, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	config.HMACKey, err = types.DeriveHMACKey(os.Getenv("HMAC_SECRET"))
	if err != nil {
		log.Fatal(err)
	}

	if err := config.Validate(); err != nil {
		log.Fatal(err)
	}

	return config
}

/* Every XX seconds, try to send new ticket emails. */
func RunNewMails(ctx *config.AppContext) {
	/* Wait a bit, so server can start up */
	time.Sleep(4 * time.Second)
	ctx.Infos.Println("Starting up mailer job...")
	for true {
		emails.CheckForNewMails(ctx)
		time.Sleep(time.Duration(ctx.Env.MailerJob) * time.Second)
	}
}

func main() {
	/* Load config from environment / .env */
	app.Env = loadConfig()
	err := run(app.Env)
	if err != nil {
		log.Fatal(err)
	}

	/* Start up Spaces (S3-compatible storage) */
	spaces.Init(app.Env.Spaces)

	/* OAuth token store (YouTube today; X tomorrow when chromedp lands) */
	if err := tokens.Init("tokens.bolt"); err != nil {
		app.Err.Fatalf("open tokens.bolt: %s", err)
	}
	if spaces.IsConfigured() {
		if err := tokens.InitRemote(app.Env.Recordings.YouTubeTokenObject, app.Env.Recordings.EncryptionKey); err != nil {
			app.Err.Printf("encrypted youtube token store disabled: %s", err)
		}
	}
	youtubepkg.Init(app.Env.YouTube.ClientID, app.Env.YouTube.ClientSecret, app.Env.YouTube.RedirectURL)

	/* Load cached data */
	getters.WaitFetch(&app)
	getters.StartWorkPool(&app)

	/* Start up Buffer */
	buffer.Init(app.Env.BufferAPI)

	/* Set up Routes + Templates */
	routes, err := handlers.Routes(&app)
	if err != nil {
		app.Err.Fatal(err)
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", app.Env.Port),
		Handler:           app.Session.LoadAndSave(routes),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	/* Kick off job to start sending mails */
	if !app.Env.MailOff {
		go RunNewMails(&app)
	}

	/* Start media card refresh after server is listening */
	if spaces.IsConfigured() {
		if mediaRendererAvailable() {
			go func() {
				time.Sleep(3 * time.Second)
				handlers.InitMediaRefresh(&app)
				app.Infos.Printf("media refresh done")
			}()
			app.Infos.Printf("scheduling media refresh")
		} else {
			app.Infos.Printf("media refresh disabled: Chrome/Chromium executable not found")
		}
	}

	handlers.StartRecordingAutopublisher(&app)

	/* Start the server */
	app.Infos.Printf("Starting application on port %s\n", app.Env.Port)
	app.Infos.Printf("... Current domain is %s\n", app.Env.GetDomain())
	err = srv.ListenAndServe()
	if err != nil {
		app.Err.Fatal(err)
	}
}

func run(env *types.EnvConfig) error {
	/* Load up the logfile */
	var logfile *os.File
	var err error
	if env.LogFile != "" {
		fmt.Println("Using logfile:", env.LogFile)
		logfile, err = os.OpenFile(env.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			return err
		}
	} else {
		fmt.Println("Using logfile: stdout")
		logfile = os.Stdout
	}

	app.Infos = log.New(logfile, "INFO\t", log.Ldate|log.Ltime)
	app.Err = log.New(logfile, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// Initialize the application configuration
	app.InProduction = env.Prod
	app.EmailCache = make(map[string]*texttemplate.Template)

	app.Infos.Println("")
	app.Infos.Println("~~~~app restarted, here we go~~~~~")
	app.Infos.Println("Running in prod?", env.Prod)

	// Initialize the session manager backed by a BoltDB file so
	// admin/dashboard logins survive app restarts. Default
	// memstore wipes every session when the process exits — fine
	// for the legacy CheckPin model (one shared PIN, re-typed
	// after every restart) but a regression for the magic-link
	// auth flow where a restart silently logs everyone out.
	// No defer Close — the DB lives for the lifetime of the
	// process. A defer here would fire when run() returns (i.e.
	// before the HTTP server even starts handling requests),
	// shuttering the store and breaking every session-touching
	// route with "database not open." On process exit the OS
	// reclaims the file handle; bbolt's mmap+commit format is
	// crash-consistent, so an unclean shutdown is recoverable on
	// the next open.
	sessDB, err := bolt.Open("sessions.bolt", 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		app.Err.Fatalf("open sessions.bolt: %s", err)
	}
	app.Session = scs.New()
	app.Session.Lifetime = 4 * 24 * time.Hour
	// Use an app-specific cookie name. The SCS default is "session",
	// which is easy for another localhost service to overwrite because
	// browser cookies are scoped by host, not port.
	app.Session.Cookie.Name = "btcpp_session"
	app.Session.Cookie.Persist = true
	app.Session.Cookie.SameSite = http.SameSiteLaxMode
	app.Session.Cookie.Secure = app.InProduction
	app.Session.Store = boltstore.New(sessDB)

	app.Notion = &types.Notion{Config: &env.Notion}
	app.Notion.Setup(env.Notion.Token)
	if getters.UsePostgresBackend(&app) {
		pool, err := db.Open(context.Background(), env.DatabaseURL)
		if err != nil {
			return err
		}
		app.DB = pool
	}

	// Per-request Notion timing is noisy in production, so keep it opt-in.
	// Recent-call tracking for /api/cache-stats remains enabled separately.
	if env.NotionRequestLogs {
		types.SetNotionRequestLogger(app.Infos.Printf)
	} else {
		types.SetNotionRequestLogger(nil)
	}

	return nil
}

func mediaRendererAvailable() bool {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	for _, path := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
