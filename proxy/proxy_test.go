package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestParseQuery verifies that the search filter parser compiles correct SQL clauses.
func TestParseQuery(t *testing.T) {
	tests := []struct {
		query    string
		wantSql  string
		wantArgs []any
	}{
		{
			query:    "set:kld r:common",
			wantSql:  " AND set_code = ? AND raw_json LIKE ?",
			wantArgs: []any{"kld", `%"rarity":"common"%`},
		},
		{
			query:    "s:mb1 r:common c:w",
			wantSql:  " AND set_code = ? AND raw_json LIKE ? AND raw_json LIKE ?",
			wantArgs: []any{"mb1", `%"rarity":"common"%`, `%"W"%`},
		},
		{
			query:    "-t:basic s:kld",
			wantSql:  " AND raw_json NOT LIKE ? AND set_code = ?",
			wantArgs: []any{`%"type_line":"%basic%"%`, "kld"},
		},
		{
			query:    "Lightning Bolt",
			wantSql:  " AND name_clean LIKE ? AND name_clean LIKE ?",
			wantArgs: []any{"%lightning%", "%bolt%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			gotSql, gotArgs := parseQuery(tt.query)
			if gotSql != tt.wantSql {
				t.Errorf("parseQuery() gotSql = %v, want %v", gotSql, tt.wantSql)
			}
			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("parseQuery() gotArgs len = %v, want %v", len(gotArgs), len(tt.wantArgs))
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("parseQuery() gotArgs[%d] = %v, want %v", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

// BenchmarkGetByNamed measures the lookup speed under exact index match vs prefix range match.
func BenchmarkGetByNamed(b *testing.B) {
	dbPath := "scryfall.db"
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		b.Skipf("Skipping benchmark: database '%s' not found. Run --update first.", dbPath)
	}

	repo, err := NewSQLiteRepository(dbPath)
	if err != nil {
		b.Fatalf("Failed to open repo: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()

	b.Run("ExactMatch", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := repo.GetByNamed(ctx, "Lightning Bolt", "")
			if err != nil && err != sql.ErrNoRows {
				b.Fatalf("query failed: %v", err)
			}
		}
	})

	b.Run("PrefixMatch", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := repo.GetByNamed(ctx, "Lightn", "")
			if err != nil && err != sql.ErrNoRows {
				b.Fatalf("query failed: %v", err)
			}
		}
	})
}

// BenchmarkSaveBatchSizes measures the insertion throughput under different transaction chunk sizes.
func BenchmarkSaveBatchSizes(b *testing.B) {
	tempDB := "test_ingest.db"
	defer os.Remove(tempDB)

	repo, err := NewSQLiteRepository(tempDB)
	if err != nil {
		b.Fatalf("failed to init repo: %v", err)
	}
	defer repo.Close()

	if err := repo.Init(context.Background()); err != nil {
		b.Fatalf("failed to init schema: %v", err)
	}

	// Generate 5000 dummy cards
	dummyCards := make([]IngestionCard, 5000)
	for i := 0; i < 5000; i++ {
		dummyCards[i] = IngestionCard{
			ID:              fmt.Sprintf("id-%d-%d", i, rand.Intn(100000)),
			Name:            fmt.Sprintf("Card Name %d", i),
			Set:             "TST",
			CollectorNumber: fmt.Sprintf("%d", i),
			Lang:            "en",
			RawJSON:         []byte(`{"object":"card","name":"Card Name","rarity":"common"}`),
		}
	}

	sizes := []int{500, 1000, 2000, 5000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("BatchSize-%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Clear cards to prevent database size inflation skewing results
				_, _ = repo.db.Exec("DELETE FROM cards")

				ctx := context.Background()
				// Slice and save in chunks
				for start := 0; start < len(dummyCards); start += size {
					end := start + size
					if end > len(dummyCards) {
						end = len(dummyCards)
					}
					err := repo.SaveBatch(ctx, dummyCards[start:end])
					if err != nil {
						b.Fatalf("batch save failed: %v", err)
					}
				}
			}
		})
	}
}

type MockRepository struct{}

func (m *MockRepository) Init(ctx context.Context) error {
	return nil
}

func (m *MockRepository) SaveBatch(ctx context.Context, cards []IngestionCard) error {
	return nil
}

func (m *MockRepository) GetByID(ctx context.Context, id string) ([]byte, error) {
	if id == "NotFoundID" {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","id":"` + id + `"}`), nil
}

func (m *MockRepository) GetByNamed(ctx context.Context, fuzzy string, setCode string) ([]byte, error) {
	if fuzzy == "NotFoundCard" {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","name":"` + fuzzy + `"}`), nil
}

func (m *MockRepository) GetBySetCol(ctx context.Context, setCode, colNum, lang string) ([]byte, error) {
	if setCode == "notfound" {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","set":"` + setCode + `","collector_number":"` + colNum + `"}`), nil
}

func (m *MockRepository) GetRandom(ctx context.Context, qParam string) ([]byte, error) {
	if strings.Contains(qParam, "NotFound") {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","name":"Random Card"}`), nil
}

func (m *MockRepository) Search(ctx context.Context, qParam, unique string) ([]byte, error) {
	if strings.Contains(qParam, "NotFound") {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"list","data":[]}`), nil
}

func (m *MockRepository) Close() error {
	return nil
}

func TestServerEndpoints(t *testing.T) {
	mockRepo := &MockRepository{}
	server := NewServer(mockRepo, 8000)

	tests := []struct {
		name           string
		method         string
		url            string
		body           string
		wantStatusCode int
		wantBody       string
	}{
		{
			name:           "Named endpoint fuzzy match",
			method:         "GET",
			url:            "/cards/named?fuzzy=Lightning+Bolt",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","name":"Lightning Bolt"}`,
		},
		{
			name:           "Named endpoint exact match",
			method:         "GET",
			url:            "/cards/named?exact=Lightning+Bolt",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","name":"Lightning Bolt"}`,
		},
		{
			name:           "Random endpoint match",
			method:         "GET",
			url:            "/cards/random?q=set:kld",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","name":"Random Card"}`,
		},
		{
			name:           "Fallback Set/Collector URL pattern",
			method:         "GET",
			url:            "/cards/kld/128",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","set":"kld","collector_number":"128"}`,
		},
		{
			name:           "Fallback UUID pattern",
			method:         "GET",
			url:            "/cards/uuid-123-abc",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","id":"uuid-123-abc"}`,
		},
		{
			name:           "Batch POST endpoint",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":["https://api.scryfall.com/cards/named?fuzzy=Lightning%20Bolt","https://api.scryfall.com/cards/uuid-123-abc"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"object":"card","name":"Lightning Bolt"},{"object":"card","id":"uuid-123-abc"}]`,
		},
		{
			name:           "Search endpoint match",
			method:         "GET",
			url:            "/cards/search?q=t:planeswalker",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"list","data":[]}`,
		},
		{
			name:           "Search endpoint missing q",
			method:         "GET",
			url:            "/cards/search",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       `{"code":"bad_request","details":"Missing 'q' query parameter","object":"error","status":400}`,
		},
		{
			name:           "Batch endpoint malformed body",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":`,
			wantStatusCode: http.StatusBadRequest,
			wantBody:       `{"code":"bad_request","details":"Invalid JSON body","object":"error","status":400}`,
		},
		{
			name:           "Named endpoint missing fuzzy",
			method:         "GET",
			url:            "/cards/named",
			wantStatusCode: http.StatusBadRequest,
			wantBody:       `{"code":"bad_request","details":"Missing 'fuzzy' or 'exact' query parameter","object":"error","status":400}`,
		},
		{
			name:           "Named endpoint card not found",
			method:         "GET",
			url:            "/cards/named?fuzzy=NotFoundCard",
			wantStatusCode: http.StatusNotFound,
			wantBody:       `{"code":"bad_request","details":"Card not found matching query: NotFoundCard","object":"error","status":404}`,
		},
		{
			name:           "Fallback Set/Collector card not found",
			method:         "GET",
			url:            "/cards/notfound/128",
			wantStatusCode: http.StatusNotFound,
			wantBody:       `{"code":"bad_request","details":"Card not found matching Set: notfound, Col: 128, Lang: en","object":"error","status":404}`,
		},
		{
			name:           "Fallback UUID card not found",
			method:         "GET",
			url:            "/cards/NotFoundID",
			wantStatusCode: http.StatusNotFound,
			wantBody:       `{"code":"bad_request","details":"Card not found with ID: NotFoundID","object":"error","status":404}`,
		},
		{
			name:           "Random endpoint card not found",
			method:         "GET",
			url:            "/cards/random?q=NotFoundFilter",
			wantStatusCode: http.StatusInternalServerError,
			wantBody:       `{"code":"bad_request","details":"Could not retrieve random card: sql: no rows in result set","object":"error","status":500}`,
		},
		{
			name:           "Search endpoint card not found",
			method:         "GET",
			url:            "/cards/search?q=NotFoundFilter",
			wantStatusCode: http.StatusInternalServerError,
			wantBody:       `{"code":"bad_request","details":"sql: no rows in result set","object":"error","status":500}`,
		},
		{
			name:           "Batch POST endpoint with failure elements",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":["https://api.scryfall.com/cards/named?fuzzy=NotFoundCard"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"code":"not_found","details":"Card not found for: https://api.scryfall.com/cards/named?fuzzy=NotFoundCard","object":"error","status":404}]`,
		},
		{
			name:           "Batch POST endpoint with lang element",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":["https://api.scryfall.com/cards/sld/901/fr"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"object":"card","set":"sld","collector_number":"901"}]`,
		},
		{
			name:           "Batch POST endpoint with malformed URL",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":[":"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"code":"not_found","details":"Card not found for: :","object":"error","status":404}]`,
		},
		{
			name:           "Batch POST endpoint with unsupported pattern",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":["https://api.scryfall.com/cards/named"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"code":"not_found","details":"Card not found for: https://api.scryfall.com/cards/named","object":"error","status":404}]`,
		},
		{
			name:           "Batch POST endpoint with generic unsupported URL",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":["https://api.scryfall.com/other/path"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"code":"not_found","details":"Card not found for: https://api.scryfall.com/other/path","object":"error","status":404}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.url, bodyReader)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("expected status %d, got %d", tt.wantStatusCode, resp.StatusCode)
			}

			respBody, _ := io.ReadAll(resp.Body)
			trimmedResp := strings.TrimSpace(string(respBody))
			if trimmedResp != tt.wantBody {
				t.Errorf("expected body %q, got %q", tt.wantBody, trimmedResp)
			}
		})
	}
}

func TestSQLiteRepository(t *testing.T) {
	dbFile := "test_repo.db"
	defer os.Remove(dbFile)

	repo, err := NewSQLiteRepository(dbFile)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	cards := []IngestionCard{
		{
			ID:              "id-bolt",
			Name:            "Lightning Bolt",
			Set:             "sld",
			CollectorNumber: "901",
			Lang:            "en",
			RawJSON:         []byte(`{"object":"card","id":"id-bolt","name":"Lightning Bolt","rarity":"common","colors":["R"],"type_line":"Instant"}`),
		},
		{
			ID:              "id-lotus",
			Name:            "Black Lotus",
			Set:             "vma",
			CollectorNumber: "4",
			Lang:            "en",
			RawJSON:         []byte(`{"object":"card","id":"id-lotus","name":"Black Lotus","rarity":"mythic","colors":[],"type_line":"Artifact"}`),
		},
	}

	if err := repo.SaveBatch(ctx, cards); err != nil {
		t.Fatalf("SaveBatch failed: %v", err)
	}

	bytes, err := repo.GetByID(ctx, "id-bolt")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if !strings.Contains(string(bytes), "Lightning Bolt") {
		t.Errorf("GetByID returned wrong data: %s", string(bytes))
	}

	bytesCache, err := repo.GetByID(ctx, "id-bolt")
	if err != nil {
		t.Fatalf("GetByID cache hit failed: %v", err)
	}
	if string(bytesCache) != string(bytes) {
		t.Errorf("GetByID cache did not match")
	}

	bytes, err = repo.GetByNamed(ctx, "Lightning Bolt", "")
	if err != nil {
		t.Fatalf("GetByNamed exact failed: %v", err)
	}
	if !strings.Contains(string(bytes), "id-bolt") {
		t.Errorf("GetByNamed exact returned wrong data")
	}

	bytes, err = repo.GetByNamed(ctx, "Lightning Bolt", "sld")
	if err != nil {
		t.Fatalf("GetByNamed exact with set failed: %v", err)
	}

	bytes, err = repo.GetByNamed(ctx, "Lightn", "")
	if err != nil {
		t.Fatalf("GetByNamed prefix failed: %v", err)
	}

	bytes, err = repo.GetByNamed(ctx, "Lightn", "sld")
	if err != nil {
		t.Fatalf("GetByNamed prefix with set failed: %v", err)
	}

	bytes, err = repo.GetBySetCol(ctx, "sld", "901", "en")
	if err != nil {
		t.Fatalf("GetBySetCol failed: %v", err)
	}
	if !strings.Contains(string(bytes), "id-bolt") {
		t.Errorf("GetBySetCol returned wrong data")
	}

	bytes, err = repo.GetBySetCol(ctx, "sld", "901", "fr")
	if err != nil {
		t.Fatalf("GetBySetCol fallback failed: %v", err)
	}

	bytes, err = repo.GetRandom(ctx, "set:sld")
	if err != nil {
		t.Fatalf("GetRandom failed: %v", err)
	}
	if !strings.Contains(string(bytes), "id-bolt") {
		t.Errorf("GetRandom returned wrong card")
	}

	bytes, err = repo.Search(ctx, "set:vma", "")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if !strings.Contains(string(bytes), "Black Lotus") {
		t.Errorf("Search returned wrong result: %s", string(bytes))
	}

	// Test Search set + type
	bytes, err = repo.Search(ctx, "set:vma t:artifact", "")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Test Search type only
	bytes, err = repo.Search(ctx, "t:artifact", "")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Test Search neither (name clean fallback)
	bytes, err = repo.Search(ctx, "Black", "")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
}

func TestCardUnmarshalJSON(t *testing.T) {
	cardData := []byte(`{
		"id": "abc-123",
		"name": "Black Lotus",
		"set": "vma",
		"collector_number": "4",
		"lang": "en"
	}`)

	var card IngestionCard
	if err := json.Unmarshal(cardData, &card); err != nil {
		t.Fatalf("failed to unmarshal IngestionCard: %v", err)
	}

	if card.ID != "abc-123" || card.Name != "Black Lotus" || card.Set != "vma" || card.CollectorNumber != "4" || card.Lang != "en" {
		t.Errorf("IngestionCard fields were not mapped correctly: %+v", card)
	}

	if string(card.RawJSON) != string(cardData) {
		t.Errorf("RawJSON did not preserve raw bytes")
	}
}

func TestCopyFile(t *testing.T) {
	src := "test_src.txt"
	dst := "test_dst.txt"
	defer os.Remove(src)
	defer os.Remove(dst)

	content := "Hello, copy!"
	if err := os.WriteFile(src, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}

	if string(got) != content {
		t.Errorf("copied content mismatch: got %q, want %q", string(got), content)
	}

	// Test CopyFile error path (non-existent source)
	if err := CopyFile("nonexistent.txt", "dst.txt"); err == nil {
		t.Errorf("expected CopyFile to fail for non-existent source")
	}

	// Test CopyFile error path (destination is a directory)
	if err := CopyFile(src, "."); err == nil {
		t.Errorf("expected CopyFile to fail when destination is a directory")
	}
}

func TestRulingsPassthrough(t *testing.T) {
	mockScryfall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cards/abc-123/rulings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"source":"wotc","comment":"Mock Ruling Comment"}]}`))
	}))
	defer mockScryfall.Close()

	oldBase := scryfallBaseURL
	scryfallBaseURL = mockScryfall.URL
	defer func() { scryfallBaseURL = oldBase }()

	mockRepo := &MockRepository{}
	server := NewServer(mockRepo, 8000)

	req := httptest.NewRequest("GET", "/cards/abc-123/rulings", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Mock Ruling Comment") {
		t.Errorf("unexpected body content: %s", string(body))
	}
}

func TestUpdateDatabase(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bulk-data" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fmt.Sprintf(`{
				"object": "list",
				"data": [
					{
						"type": "default_cards",
						"download_uri": "%s/default-cards.json"
					}
				]
			}`, "http://"+r.Host)))
			return
		}
		if r.URL.Path == "/default-cards.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[
				{
					"id": "abc-123",
					"name": "Black Lotus",
					"set": "vma",
					"collector_number": "4",
					"lang": "en"
				}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	oldManifestURL := bulkDataManifestURL
	bulkDataManifestURL = mockServer.URL + "/bulk-data"
	defer func() { bulkDataManifestURL = oldManifestURL }()

	dbPath := "test_update.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + ".tmp")

	ctx := context.Background()
	if err := UpdateDatabase(ctx, dbPath); err != nil {
		t.Fatalf("UpdateDatabase failed: %v", err)
	}

	repo, err := NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to open updated DB: %v", err)
	}
	defer repo.Close()

	bytes, err := repo.GetByID(ctx, "abc-123")
	if err != nil {
		t.Fatalf("failed to query card from updated DB: %v", err)
	}
	if !strings.Contains(string(bytes), "Black Lotus") {
		t.Errorf("card was not correctly imported: %s", string(bytes))
	}
}

func TestServerStart(t *testing.T) {
	mockRepo := &MockRepository{}
	server := NewServer(mockRepo, 0)
	go func() {
		_ = server.Start()
	}()
	time.Sleep(10 * time.Millisecond)
}


