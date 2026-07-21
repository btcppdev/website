package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	texttemplate "text/template"
	"time"

	"btcpp-web/external/buffer"
	"btcpp-web/external/spaces"
	"btcpp-web/external/tokens"
	youtubepkg "btcpp-web/external/youtube"
	"btcpp-web/internal/config"
	"btcpp-web/internal/db"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/handlers"
	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
)

var app config.AppContext

const mailerStartupDelay = 8 * time.Minute

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
	ctx.Infos.Printf("Mailer job waiting %s before first run...", mailerStartupDelay)
	time.Sleep(mailerStartupDelay)
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

	/* Start up Buffer */
	buffer.Init(app.Env.BufferAPI)

	/* Set up Routes + Templates */
	routes, err := handlers.Routes(&app)
	if err != nil {
		app.Err.Fatal(err)
	}
	sessionHandler := app.Session.LoadAndSave(routes)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || strings.HasSuffix(r.URL.Path, "/run-of-show/events") {
			routes.ServeHTTP(w, r)
			return
		}
		sessionHandler.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", app.Env.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	/* Kick off job to start sending mails */
	if !app.Env.MailOff {
		go RunNewMails(&app)
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

	pool, err := db.Open(context.Background(), env.DatabaseURL)
	if err != nil {
		return err
	}
	app.DB = pool
	applied, err := db.Migrate(context.Background(), pool, app.Infos)
	if err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	if applied == 0 {
		app.Infos.Println("database migrations up to date")
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
	app.Session.Store = pgxstore.New(app.DB)
	app.Infos.Println("using postgres session store")

	return nil
}
