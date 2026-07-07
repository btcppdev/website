package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/types"
)

func main() {
	email := flag.String("email", "dev-admin@example.test", "email address for the local login link")
	next := flag.String("next", "/admin", "relative path to visit after login")
	flag.Parse()

	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	if env.Prod {
		log.Fatal("refusing to mint dev login link while PROD=true")
	}
	env.HMACKey, err = types.DeriveHMACKey(os.Getenv("HMAC_SECRET"))
	if err != nil {
		log.Fatal(err)
	}
	if err := env.Validate(); err != nil {
		log.Fatal(err)
	}

	ctx := &config.AppContext{Env: env}
	fmt.Println(auth.MagicLink(ctx, *email, *next))
}
