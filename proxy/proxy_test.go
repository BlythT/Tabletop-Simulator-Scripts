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
	"os/exec"
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
			wantSql:  " AND set_code = ? AND json_extract(raw_json, '$.rarity') = ?",
			wantArgs: []any{"kld", "common"},
		},
		{
			query:    "s:mb1 r:common c:w",
			wantSql:  " AND set_code = ? AND json_extract(raw_json, '$.rarity') = ? AND json_extract(raw_json, '$.colors') LIKE ?",
			wantArgs: []any{"mb1", "common", "%W%"},
		},
		{
			query:    "-t:basic s:kld",
			wantSql:  " AND json_extract(raw_json, '$.type_line') NOT LIKE ? AND set_code = ?",
			wantArgs: []any{"%basic%", "kld"},
		},
		{
			query:    "Lightning Bolt",
			wantSql:  " AND name_clean LIKE ? AND name_clean LIKE ?",
			wantArgs: []any{"%lightning%", "%bolt%"},
		},
		{
			query:    "-set:kld -r:common",
			wantSql:  " AND set_code != ? AND json_extract(raw_json, '$.rarity') != ?",
			wantArgs: []any{"kld", "common"},
		},
		{
			query:    "  SET:KLD   -r:CoMmOn  ",
			wantSql:  " AND set_code = ? AND json_extract(raw_json, '$.rarity') != ?",
			wantArgs: []any{"kld", "common"},
		},
		{
			query:    "set:kld+r:common",
			wantSql:  " AND set_code = ? AND json_extract(raw_json, '$.rarity') = ?",
			wantArgs: []any{"kld", "common"},
		},
		{
			query:    "   ",
			wantSql:  "",
			wantArgs: nil,
		},
		{
			query:    "invalid:tag set:kld",
			wantSql:  " AND set_code = ?",
			wantArgs: []any{"kld"},
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
	isTemp := false

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Fallback to a generated temporary database for CI and clean environments
		dbPath = "test_benchmark.db"
		isTemp = true
		repo, err := NewSQLiteRepository(dbPath)
		if err != nil {
			b.Fatalf("failed to init repo: %v", err)
		}
		if err := repo.Init(context.Background()); err != nil {
			b.Fatalf("failed to init schema: %v", err)
		}

		// Base JSON template based on a real Scryfall card dump (Forest from Bloomburrow)
		// We replace the name to make it unique per row
		baseJSONTmpl := `{"object":"card","id":"0000419b-0bba-4488-8f7a-6194544ce91e","oracle_id":"b34bb2dc-c1af-4d77-b0b3-a0fb342a5fc6","multiverse_ids":[668564],"name":"%s","lang":"en","released_at":"2024-08-02","layout":"normal","highres_image":true,"image_status":"highres_scan","image_uris":{"small":"https://cards.scryfall.io/small/front/0/0/0000419b-0bba-4488-8f7a-6194544ce91e.jpg?1721427487","normal":"https://cards.scryfall.io/normal/front/0/0/0000419b-0bba-4488-8f7a-6194544ce91e.jpg?1721427487","large":"https://cards.scryfall.io/large/front/0/0/0000419b-0bba-4488-8f7a-6194544ce91e.jpg?1721427487"},"mana_cost":"","cmc":0.0,"type_line":"Basic Land — Forest","oracle_text":"({T}: Add {G}.)","colors":[],"color_identity":["G"],"keywords":[],"produced_mana":["G"],"legalities":{"standard":"legal","future":"legal","historic":"legal"},"games":["paper","mtgo","arena"],"reserved":false,"foil":true,"nonfoil":true,"finishes":["nonfoil","foil"],"oversized":false,"promo":false,"reprint":true,"variation":false,"set_id":"a2f58272-bba6-439d-871e-7a46686ac018","set":"blb","set_name":"Bloomburrow","set_type":"expansion","collector_number":"280","digital":false,"rarity":"common","border_color":"black","frame":"2015","full_art":true,"textless":false,"booster":true,"story_spotlight":false}`

		// Generate 10,000 dummy cards for a reasonable index size
		dummyCards := make([]IngestionCard, 10000)
		for i := 0; i < 9999; i++ {
			name := fmt.Sprintf("%c Card %d", 'A'+rune(rand.Intn(26)), i)
			dummyCards[i] = IngestionCard{
				ID:              fmt.Sprintf("id-%d", i),
				Name:            name,
				Set:             "TST",
				CollectorNumber: fmt.Sprintf("%d", i),
				Lang:            "en",
				RawJSON:         []byte(fmt.Sprintf(baseJSONTmpl, name)),
			}
		}
		// Add the target card we will search for
		dummyCards[9999] = IngestionCard{
			ID:              "id-bolt",
			Name:            "Lightning Bolt",
			Set:             "TST",
			CollectorNumber: "9999",
			Lang:            "en",
			RawJSON:         []byte(fmt.Sprintf(baseJSONTmpl, "Lightning Bolt")),
		}

		if err := repo.SaveBatch(context.Background(), dummyCards); err != nil {
			b.Fatalf("batch save failed: %v", err)
		}
		repo.Close()
	}

	if isTemp {
		defer os.Remove(dbPath)
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

	baseJSONTmpl := `{"object":"card","id":"0000419b-0bba-4488-8f7a-6194544ce91e","oracle_id":"b34bb2dc-c1af-4d77-b0b3-a0fb342a5fc6","multiverse_ids":[668564],"name":"%s","lang":"en","released_at":"2024-08-02","layout":"normal","highres_image":true,"image_status":"highres_scan","image_uris":{"small":"https://cards.scryfall.io/small/front/0/0/0000419b-0bba-4488-8f7a-6194544ce91e.jpg?1721427487","normal":"https://cards.scryfall.io/normal/front/0/0/0000419b-0bba-4488-8f7a-6194544ce91e.jpg?1721427487","large":"https://cards.scryfall.io/large/front/0/0/0000419b-0bba-4488-8f7a-6194544ce91e.jpg?1721427487"},"mana_cost":"","cmc":0.0,"type_line":"Basic Land — Forest","oracle_text":"({T}: Add {G}.)","colors":[],"color_identity":["G"],"keywords":[],"produced_mana":["G"],"legalities":{"standard":"legal","future":"legal","historic":"legal"},"games":["paper","mtgo","arena"],"reserved":false,"foil":true,"nonfoil":true,"finishes":["nonfoil","foil"],"oversized":false,"promo":false,"reprint":true,"variation":false,"set_id":"a2f58272-bba6-439d-871e-7a46686ac018","set":"blb","set_name":"Bloomburrow","set_type":"expansion","collector_number":"280","digital":false,"rarity":"common","border_color":"black","frame":"2015","full_art":true,"textless":false,"booster":true,"story_spotlight":false}`

	// Generate 5000 dummy cards
	dummyCards := make([]IngestionCard, 5000)
	for i := 0; i < 5000; i++ {
		name := fmt.Sprintf("%c Card %d", 'A'+rune(rand.Intn(26)), i)
		dummyCards[i] = IngestionCard{
			ID:              fmt.Sprintf("id-%d-%d", i, rand.Intn(100000)),
			Name:            name,
			Set:             "TST",
			CollectorNumber: fmt.Sprintf("%d", i),
			Lang:            "en",
			RawJSON:         []byte(fmt.Sprintf(baseJSONTmpl, name)),
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
	return []byte(`{"object":"card","id":"` + id + `","name":"Black Lotus","type_line":"Artifact","cmc":0.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`), nil
}

func (m *MockRepository) GetByNamed(ctx context.Context, fuzzy string, setCode string) ([]byte, error) {
	if fuzzy == "NotFoundCard" {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","id":"mock-id-bolt","name":"` + fuzzy + `","type_line":"Instant","cmc":1.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`), nil
}

func (m *MockRepository) GetBySetCol(ctx context.Context, setCode, colNum, lang string) ([]byte, error) {
	if setCode == "notfound" {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","id":"mock-id-setcol","name":"Custom Card","set":"` + setCode + `","collector_number":"` + colNum + `","type_line":"Creature","cmc":3.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`), nil
}

func (m *MockRepository) GetRandom(ctx context.Context, qParam string) ([]byte, error) {
	if strings.Contains(qParam, "NotFound") {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"card","id":"mock-id-random","name":"Random Card","type_line":"Sorcery","cmc":2.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`), nil
}

func (m *MockRepository) Search(ctx context.Context, qParam, unique string) ([]byte, error) {
	if strings.Contains(qParam, "NotFound") {
		return nil, sql.ErrNoRows
	}
	return []byte(`{"object":"list","data":[]}`), nil
}

func (m *MockRepository) Reload(ctx context.Context, tempDBPath string) error {
	return nil
}

func (m *MockRepository) Close() error {
	return nil
}

func (m *MockRepository) DBPath() string {
	return "scryfall.db"
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
			wantBody:       `{"object":"card","id":"mock-id-bolt","name":"Lightning Bolt","type_line":"Instant","cmc":1.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`,
		},
		{
			name:           "Named endpoint exact match",
			method:         "GET",
			url:            "/cards/named?exact=Lightning+Bolt",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","id":"mock-id-bolt","name":"Lightning Bolt","type_line":"Instant","cmc":1.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`,
		},
		{
			name:           "Random endpoint match",
			method:         "GET",
			url:            "/cards/random?q=set:kld",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","id":"mock-id-random","name":"Random Card","type_line":"Sorcery","cmc":2.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`,
		},
		{
			name:           "Fallback Set/Collector URL pattern",
			method:         "GET",
			url:            "/cards/kld/128",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","id":"mock-id-setcol","name":"Custom Card","set":"kld","collector_number":"128","type_line":"Creature","cmc":3.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`,
		},
		{
			name:           "Fallback UUID pattern",
			method:         "GET",
			url:            "/cards/uuid-123-abc",
			wantStatusCode: http.StatusOK,
			wantBody:       `{"object":"card","id":"uuid-123-abc","name":"Black Lotus","type_line":"Artifact","cmc":0.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}`,
		},
		{
			name:           "Batch POST endpoint",
			method:         "POST",
			url:            "/batch",
			body:           `{"urls":["https://api.scryfall.com/cards/named?fuzzy=Lightning%20Bolt","https://api.scryfall.com/cards/uuid-123-abc"]}`,
			wantStatusCode: http.StatusOK,
			wantBody:       `[{"object":"card","id":"mock-id-bolt","name":"Lightning Bolt","type_line":"Instant","cmc":1.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}},{"object":"card","id":"uuid-123-abc","name":"Black Lotus","type_line":"Artifact","cmc":0.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}]`,
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
			wantBody:       `[{"object":"card","id":"mock-id-setcol","name":"Custom Card","set":"sld","collector_number":"901","type_line":"Creature","cmc":3.0,"oracle_text":"","layout":"normal","image_uris":{"normal":"http://127.0.0.1/normal.jpg"}}]`,
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

	// Test Reload error (rename failure)
	if err := repo.Reload(ctx, "nonexistent.db"); err == nil {
		t.Errorf("expected Reload to fail for non-existent temp file")
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

func TestAdminUpdateFlow(t *testing.T) {
	// Mock bulk manifest and default-cards data
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
					"id": "lotus-999",
					"name": "Black Lotus Ingested",
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

	// Create real repository to test actual Reload and file swap under lock!
	dbPath := "test_admin_update.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + ".tmp")

	repo, err := NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	server := NewServer(repo, 0)

	// Test GET /admin/update/status when idle
	reqStatus := httptest.NewRequest("GET", "/admin/update/status", nil)
	reqStatus.RemoteAddr = "127.0.0.1:1234"
	wStatus := httptest.NewRecorder()
	server.ServeHTTP(wStatus, reqStatus)
	if wStatus.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", wStatus.Result().StatusCode)
	}
	var statusMap map[string]string
	json.NewDecoder(wStatus.Body).Decode(&statusMap)
	if statusMap["status"] != "idle" {
		t.Errorf("expected status idle, got %s", statusMap["status"])
	}

	// Test POST /admin/update
	reqUpdate := httptest.NewRequest("POST", "/admin/update", nil)
	reqUpdate.RemoteAddr = "127.0.0.1:1234"
	wUpdate := httptest.NewRecorder()
	server.ServeHTTP(wUpdate, reqUpdate)
	if wUpdate.Result().StatusCode != http.StatusAccepted {
		t.Errorf("expected 202 Accepted, got %d", wUpdate.Result().StatusCode)
	}

	// Try triggering again to verify 409 Conflict
	wConflict := httptest.NewRecorder()
	server.ServeHTTP(wConflict, reqUpdate)
	if wConflict.Result().StatusCode != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", wConflict.Result().StatusCode)
	}

	// Poll status until it is no longer running (wait up to 5 seconds)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		wPoll := httptest.NewRecorder()
		server.ServeHTTP(wPoll, reqStatus)
		var m map[string]string
		json.NewDecoder(wPoll.Body).Decode(&m)
		if strings.HasPrefix(m["status"], "success") {
			break
		}
		if strings.HasPrefix(m["status"], "failed") {
			t.Fatalf("update failed: %s", m["status"])
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify that the new database Lotus card exists!
	bytes, err := repo.GetByID(ctx, "lotus-999")
	if err != nil {
		t.Fatalf("failed to get card after reload: %v", err)
	}
	if !strings.Contains(string(bytes), "Black Lotus Ingested") {
		t.Errorf("unexpected card json content: %s", string(bytes))
	}
}

func TestAdminUpdateFailure(t *testing.T) {
	oldManifestURL := bulkDataManifestURL
	bulkDataManifestURL = "http://invalid-domain-should-fail"
	defer func() { bulkDataManifestURL = oldManifestURL }()

	repo, err := NewSQLiteRepository("test_fail.db")
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}
	defer repo.Close()
	defer os.Remove("test_fail.db")

	server := NewServer(repo, 0)

	// Trigger update
	reqUpdate := httptest.NewRequest("POST", "/admin/update", nil)
	reqUpdate.RemoteAddr = "127.0.0.1:1234"
	wUpdate := httptest.NewRecorder()
	server.ServeHTTP(wUpdate, reqUpdate)

	// Wait up to 5 seconds for status to change to "failed"
	deadline := time.Now().Add(5 * time.Second)
	reqStatus := httptest.NewRequest("GET", "/admin/update/status", nil)
	reqStatus.RemoteAddr = "127.0.0.1:1234"
	for time.Now().Before(deadline) {
		wPoll := httptest.NewRecorder()
		server.ServeHTTP(wPoll, reqStatus)
		var m map[string]string
		json.NewDecoder(wPoll.Body).Decode(&m)
		if strings.HasPrefix(m["status"], "failed") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestLuaE2EIntegration(t *testing.T) {
	_, err := exec.LookPath("lua")
	if err != nil {
		t.Skip("lua interpreter not found in path, skipping E2E integration test")
	}

	mockRepo := &MockRepository{}
	server := NewServer(mockRepo, 0)
	ts := httptest.NewServer(server)
	defer ts.Close()

	cmd := exec.Command("lua", "Magic/importer_test_runner.lua", ts.URL)
	cmd.Dir = ".." // Run from project root folder where Magic/ is located

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Lua E2E integration test failed: %v\nOutput:\n%s", err, string(output))
	}

	t.Logf("Lua E2E test completed successfully!\nOutput:\n%s", string(output))
}

func TestAdminUpdateForbidden(t *testing.T) {
	repo := &MockRepository{}
	server := NewServer(repo, 0)

	// Test GET /admin/update/status from non-loopback IP
	reqStatus := httptest.NewRequest("GET", "/admin/update/status", nil)
	reqStatus.RemoteAddr = "192.168.1.50:1234" // Non-loopback
	wStatus := httptest.NewRecorder()
	server.ServeHTTP(wStatus, reqStatus)
	if wStatus.Result().StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", wStatus.Result().StatusCode)
	}

	// Test POST /admin/update from non-loopback IP
	reqUpdate := httptest.NewRequest("POST", "/admin/update", nil)
	reqUpdate.RemoteAddr = "192.168.1.50:1234"
	wUpdate := httptest.NewRecorder()
	server.ServeHTTP(wUpdate, reqUpdate)
	if wUpdate.Result().StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", wUpdate.Result().StatusCode)
	}
}

func TestNoFullTableScans(t *testing.T) {
	dbFile := "test_explain_no_scan.db"
	defer os.Remove(dbFile)

	repo, err := NewSQLiteRepository(dbFile)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	queries := []struct {
		name      string
		query     string
		args      []any
		allowScan bool
	}{
		{
			name:  "GetByID",
			query: QueryGetByID,
			args:  []any{"test-id"},
		},
		{
			name:  "GetByNamed (Exact)",
			query: QueryGetByNamedExact,
			args:  []any{"lightningbolt"},
		},
		{
			name:  "GetByNamed (Exact + Set)",
			query: QueryGetByNamedExactSet,
			args:  []any{"lightningbolt", "eld"},
		},
		{
			name:  "GetByNamed (Prefix)",
			query: QueryGetByNamedPrefix,
			args:  []any{"lightning%"},
		},
		{
			name:  "GetByNamed (Prefix + Set)",
			query: QueryGetByNamedPrefixSet,
			args:  []any{"lightning%", "eld"},
		},
		{
			name:  "GetBySetCol (Exact)",
			query: QueryGetBySetColLang,
			args:  []any{"eld", "123", "en"},
		},
		{
			name:  "GetBySetCol (Fallback)",
			query: QueryGetBySetCol,
			args:  []any{"eld", "123"},
		},
		{
			name:  "GetRandom (No Filters)",
			query: QueryGetRandomNoFilters,
			args:  nil,
		},
		{
			name:  "Search (Set Filter)",
			query: QueryBaseSelect + " AND set_code = ? ORDER BY LENGTH(name) ASC LIMIT 100",
			args:  []any{"eld"},
		},
		{
			name:  "Search (Rarity Filter)",
			query: QueryBaseSelect + " AND json_extract(raw_json, '$.rarity') = ? ORDER BY LENGTH(name) ASC LIMIT 100",
			args:  []any{"rare"},
		},
		{
			name:      "Search (Name Filter Infix)",
			query:     QueryBaseSelect + " AND name_clean LIKE ? ORDER BY LENGTH(name) ASC LIMIT 100",
			args:      []any{"%lightning%"},
			allowScan: true, // Infix LIKE cannot use B-Tree index, expected to scan
		},
		{
			name:      "Search (Name Filter Prefix)",
			query:     QueryBaseSelect + " AND name_clean LIKE ? ORDER BY LENGTH(name) ASC LIMIT 100",
			args:      []any{"lightning%"},
			allowScan: false, // Must use index
		},
		{
			name:      "Search (Name Filter Negated Infix)",
			query:     QueryBaseSelect + " AND name_clean NOT LIKE ? ORDER BY LENGTH(name) ASC LIMIT 100",
			args:      []any{"%lightning%"},
			allowScan: true, // Negated infix LIKE expected to scan
		},
		{
			name:      "Search (Name Filter Negated Prefix)",
			query:     QueryBaseSelect + " AND name_clean NOT LIKE ? ORDER BY LENGTH(name) ASC LIMIT 100",
			args:      []any{"lightning%"},
			allowScan: true, // Negated prefix LIKE cannot be optimized via B-tree range check, expected to scan
		},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			explainQuery := "EXPLAIN QUERY PLAN " + tc.query
			rows, err := repo.db.QueryContext(ctx, explainQuery, tc.args...)
			if err != nil {
				t.Fatalf("failed to explain query: %v", err)
			}
			defer rows.Close()

			var details []string
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatalf("failed to scan explain result: %v", err)
				}
				details = append(details, detail)

				// Only fail if it's scanning the cards table (O(N) scan) and we don't allow it.
				// SQLite in-memory / virtual scans like "SCAN CONSTANT ROW" are O(1) and acceptable.
				if !tc.allowScan && strings.Contains(detail, "SCAN ") && strings.Contains(detail, "cards") {
					t.Errorf("FAIL: Full table scan detected!\nPlan detail: %s\nQuery: %s", detail, tc.query)
				}
			}
			
			if t.Failed() {
				t.Logf("Full query plan for %s:\n\t%s", tc.name, strings.Join(details, "\n\t"))
			}
		})
	}
}
