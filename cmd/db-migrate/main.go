package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"btcpp-web/internal/db"
	"btcpp-web/internal/envconfig"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dir := flag.String("dir", "db/migrations", "directory containing SQL migration files")
	flag.Parse()

	env, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	pool, err := db.Open(ctx, env.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	logger := log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	applied, err := db.MigrateDir(ctx, pool, *dir, logger)
	if err != nil {
		log.Fatal(err)
	}
	if applied == 0 {
		logger.Println("database migrations up to date")
	}
}
