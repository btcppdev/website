package main

import (
	"context"
	"fmt"
	"log"

	"btcpp-web/internal/envconfig"
	"btcpp-web/internal/types"
)

func main() {
	c, err := envconfig.Load(".env")
	if err != nil {
		log.Fatal(err)
	}
	n := &types.Notion{}
	n.Setup(c.Notion.Token)

	db, err := n.Client.RetrieveDatabase(context.Background(), c.Notion.ConfTalkDb)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ConfTalkDb properties:")
	for k, v := range db.Properties {
		fmt.Printf("  %s (type=%s)\n", k, v.Type)
	}
}
