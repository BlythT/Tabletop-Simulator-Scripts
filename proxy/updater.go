package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const UserAgent = "TTS-Card-Importer-Proxy/1.0 (contact: github-agent)"

var bulkDataManifestURL = "https://api.scryfall.com/bulk-data"

type BulkManifestEntry struct {
	Type        string `json:"type"`
	DownloadURI string `json:"download_uri"`
}

type BulkManifest struct {
	Data []BulkManifestEntry `json:"data"`
}

// UpdateDatabase fetches the Scryfall manifest, downloads the bulk card data,
// and populates the temporary database at tempDBPath.
func UpdateDatabase(ctx context.Context, tempDBPath string) error {
	fmt.Println("Fetching Scryfall bulk data manifest...")
	
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", bulkDataManifestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch bulk manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bulk manifest HTTP error: %s", resp.Status)
	}

	var manifest BulkManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed to decode manifest JSON: %w", err)
	}

	var downloadURL string
	for _, entry := range manifest.Data {
		if entry.Type == "default_cards" {
			downloadURL = entry.DownloadURI
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("could not find default_cards download URL in manifest")
	}

	// Remove old temporary file if it exists
	_ = os.Remove(tempDBPath)

	fmt.Printf("Initializing temporary database at %s...\n", tempDBPath)
	repo, err := NewSQLiteRepositoryForIngestion(tempDBPath)
	if err != nil {
		return err
	}
	defer repo.Close()

	if err := repo.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize temp database schema: %w", err)
	}

	fmt.Printf("Downloading default cards from %s...\n", downloadURL)
	reqDownload, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return err
	}
	reqDownload.Header.Set("User-Agent", UserAgent)
	reqDownload.Header.Set("Accept", "*/*")
	
	// Default cards download might take a while, use a generous timeout
	downloadClient := &http.Client{Timeout: 10 * time.Minute}
	respDownload, err := downloadClient.Do(reqDownload)
	if err != nil {
		return fmt.Errorf("failed to download bulk data: %w", err)
	}
	defer respDownload.Body.Close()

	if respDownload.StatusCode != http.StatusOK {
		return fmt.Errorf("bulk data download HTTP error: %s", respDownload.Status)
	}

	dec := json.NewDecoder(respDownload.Body)

	// Read opening bracket '['
	t, err := dec.Token()
	if err != nil {
		return fmt.Errorf("failed to parse JSON array start: %w", err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != '[' {
		return fmt.Errorf("expected JSON array start, got: %v", t)
	}

	fmt.Println("Parsing cards and populating SQLite...")

	var batch []IngestionCard
	count := 0
	lastLogged := time.Now()

	for dec.More() {
		var card IngestionCard
		if err := dec.Decode(&card); err != nil {
			return fmt.Errorf("error parsing card JSON: %w", err)
		}

		batch = append(batch, card)
		count++

		if len(batch) >= 2000 {
			if err := repo.SaveBatch(ctx, batch); err != nil {
				return fmt.Errorf("error saving batch to database: %w", err)
			}
			batch = batch[:0]
		}

		if time.Since(lastLogged) > 5*time.Second {
			fmt.Printf("Ingested %d cards...\n", count)
			lastLogged = time.Now()
		}
	}

	// Read closing bracket ']'
	t, err = dec.Token()
	if err != nil {
		return fmt.Errorf("failed to parse JSON array end: %w", err)
	}
	if delim, ok := t.(json.Delim); !ok || delim != ']' {
		return fmt.Errorf("expected JSON array end, got: %v", t)
	}

	// Save any remaining cards in the batch
	if len(batch) > 0 {
		if err := repo.SaveBatch(ctx, batch); err != nil {
			return fmt.Errorf("error saving final batch to database: %w", err)
		}
	}

	fmt.Printf("Successfully ingested %d cards.\n", count)
	return nil
}
