package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("Fatal error: %v", err)
	}
}

func run() error {
	updateFlag := flag.Bool("update", false, "Download latest Scryfall bulk data and update the local database")
	portFlag := flag.Int("port", 8000, "Port to run the HTTP server on")
	dbFlag := flag.String("db", "scryfall.db", "Path to the SQLite database file")
	flag.Parse()

	ctx := context.Background()

	if *updateFlag {
		fmt.Println("Starting database update process...")
		tempDB := *dbFlag + ".tmp"
		if err := UpdateDatabase(ctx, tempDB); err != nil {
			return fmt.Errorf("error updating database: %w", err)
		}

		// Swap temp DB with active DB
		_ = os.Remove(*dbFlag)
		if err := os.Rename(tempDB, *dbFlag); err != nil {
			return fmt.Errorf("failed to swap database: %w", err)
		}
		fmt.Println("Database update complete!")
		return nil
	}

	// Server mode
	if _, err := os.Stat(*dbFlag); os.IsNotExist(err) {
		fmt.Printf("Warning: Database file '%s' not found.\n", *dbFlag)
		fmt.Println("Please run with '--update' first to populate the card database:")
		fmt.Printf("  go run . --update\n\n")
	}

	repo, err := NewSQLiteRepository(*dbFlag)
	if err != nil {
		return fmt.Errorf("failed to initialize database repository: %w", err)
	}
	defer repo.Close()

	if err := repo.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	server := NewServer(repo, *portFlag)
	if err := server.Start(); err != nil {
		return fmt.Errorf("server stopped with error: %w", err)
	}

	return nil
}
