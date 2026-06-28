package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	updateFlag := flag.Bool("update", false, "Download latest Scryfall bulk data and update the local database")
	portFlag := flag.Int("port", 8000, "Port to run the HTTP server on")
	dbFlag := flag.String("db", "scryfall.db", "Path to the SQLite database file")
	flag.Parse()

	ctx := context.Background()

	if *updateFlag {
		fmt.Println("Starting database update process...")
		if err := UpdateDatabase(ctx, *dbFlag); err != nil {
			log.Fatalf("Error updating database: %v", err)
		}
		os.Exit(0)
	}

	// Server mode
	if _, err := os.Stat(*dbFlag); os.IsNotExist(err) {
		fmt.Printf("Warning: Database file '%s' not found.\n", *dbFlag)
		fmt.Println("Please run with '--update' first to populate the card database:")
		fmt.Printf("  go run . --update\n\n")
	}

	repo, err := NewSQLiteRepository(*dbFlag)
	if err != nil {
		log.Fatalf("Failed to initialize database repository: %v", err)
	}
	defer repo.Close()

	if err := repo.Init(ctx); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	server := NewServer(repo, *portFlag)
	if err := server.Start(); err != nil {
		log.Fatalf("Server stopped with error: %v", err)
	}
}
