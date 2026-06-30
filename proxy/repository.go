package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	_ "github.com/glebarez/go-sqlite"
)

var (
	rxColor = regexp.MustCompile(`(?i)^c([=<>]+)(.+)$`)
	rxCI    = regexp.MustCompile(`(?i)^ci([=<>]+)(.+)$`)
	rxCMC   = regexp.MustCompile(`(?i)^(cmc|mv)([=<>]+)(.+)$`)
	rxPower = regexp.MustCompile(`(?i)^(pow|power)([=<>]+)(.+)$`)
	rxTough = regexp.MustCompile(`(?i)^(tou|toughness)([=<>]+)(.+)$`)
)

// CardRepository defines the database operations for MTG cards.
type CardRepository interface {
	Init(ctx context.Context) error
	SaveBatch(ctx context.Context, cards []IngestionCard) error
	GetByID(ctx context.Context, id string) ([]byte, error)
	GetByNamed(ctx context.Context, fuzzy string, setCode string) ([]byte, error)
	GetBySetCol(ctx context.Context, setCode, colNum, lang string) ([]byte, error)
	GetRandom(ctx context.Context, qParam string) ([]byte, error)
	Search(ctx context.Context, qParam, unique string) ([]byte, error)
	Close() error
	Reload(ctx context.Context, tempDBPath string) error
	DBPath() string
}

type SQLiteRepository struct {
	dbPath string
	db     *sql.DB
	cache  map[string][]byte
	mu     sync.RWMutex
}

func (r *SQLiteRepository) DBPath() string {
	return r.dbPath
}

// applyReadPragmas configures the connection for production read workloads.
func applyReadPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA cache_size = -64000;",
		"PRAGMA mmap_size = 1073741824;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("failed to apply pragma %q: %w", p, err)
		}
	}
	return nil
}

// applyFastWritePragmas configures the connection for bulk write/ingestion.
func applyFastWritePragmas(db *sql.DB) error {
	fastPragmas := []string{
		"PRAGMA journal_mode = OFF;",
		"PRAGMA synchronous = OFF;",
		"PRAGMA locking_mode = EXCLUSIVE;",
		"PRAGMA cache_size = -128000;",
	}
	for _, p := range fastPragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("failed to apply fast ingestion pragma %q: %w", p, err)
		}
	}
	return nil
}

func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := applyReadPragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteRepository{
		dbPath: dbPath,
		db:     db,
		cache:  make(map[string][]byte),
	}, nil
}

func NewSQLiteRepositoryForIngestion(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database for ingestion: %w", err)
	}

	if err := applyFastWritePragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteRepository{
		dbPath: dbPath,
		db:     db,
		cache:  make(map[string][]byte),
	}, nil
}

func (r *SQLiteRepository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.db.Close()
}

func (r *SQLiteRepository) Init(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cards (
			id TEXT PRIMARY KEY,
			name TEXT,
			name_clean TEXT COLLATE NOCASE,
			set_code TEXT,
			collector_number TEXT,
			lang TEXT,
			raw_json TEXT
		);
		DROP INDEX IF EXISTS idx_cards_name_clean_nocase;
		DROP INDEX IF EXISTS idx_cards_set_name_nocase;
		CREATE INDEX IF NOT EXISTS idx_cards_name_clean ON cards(name_clean);
		CREATE INDEX IF NOT EXISTS idx_cards_set_name ON cards(set_code, name_clean);
		CREATE INDEX IF NOT EXISTS idx_cards_set_col ON cards(set_code, collector_number);
		CREATE INDEX IF NOT EXISTS idx_cards_rarity ON cards(json_extract(raw_json, '$.rarity'));
		CREATE INDEX IF NOT EXISTS idx_cards_type_line ON cards(json_extract(raw_json, '$.type_line') COLLATE NOCASE);
		CREATE INDEX IF NOT EXISTS idx_cards_cmc ON cards(CAST(json_extract(raw_json, '$.cmc') AS REAL));
		CREATE INDEX IF NOT EXISTS idx_cards_lang ON cards(json_extract(raw_json, '$.lang'));
		CREATE INDEX IF NOT EXISTS idx_cards_color_identity ON cards(json_extract(raw_json, '$.color_identity'));
		CREATE INDEX IF NOT EXISTS idx_cards_power ON cards(json_extract(raw_json, '$.power'));
		CREATE INDEX IF NOT EXISTS idx_cards_toughness ON cards(json_extract(raw_json, '$.toughness'));
		CREATE INDEX IF NOT EXISTS idx_cards_artist ON cards(json_extract(raw_json, '$.artist') COLLATE NOCASE);
	`)
	return err
}

func (r *SQLiteRepository) Reload(ctx context.Context, tempDBPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. Close current db pool
	if err := r.db.Close(); err != nil {
		return fmt.Errorf("failed to close current database: %w", err)
	}

	// 2. Remove old database file
	if err := os.Remove(r.dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old database: %w", err)
	}

	// 3. Rename temp database file to active database file
	if err := os.Rename(tempDBPath, r.dbPath); err != nil {
		return fmt.Errorf("failed to rename temp database: %w", err)
	}

	// 4. Re-open database connection pool
	db, err := sql.Open("sqlite", r.dbPath)
	if err != nil {
		return fmt.Errorf("failed to re-open database: %w", err)
	}

	if err := applyReadPragmas(db); err != nil {
		db.Close()
		return fmt.Errorf("failed to re-apply pragmas after reload: %w", err)
	}
	r.db = db

	// 5. Clear the in-memory lookup cache to prevent stale data
	r.cache = make(map[string][]byte)

	return nil
}

func (r *SQLiteRepository) SaveBatch(ctx context.Context, cards []IngestionCard) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO cards (id, name, name_clean, set_code, collector_number, lang, raw_json) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, card := range cards {
		clean := cleanName(card.Name)
		_, err := stmt.ExecContext(ctx,
			card.ID,
			card.Name,
			clean,
			strings.ToLower(card.Set),
			card.CollectorNumber,
			strings.ToLower(card.Lang),
			string(card.RawJSON),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *SQLiteRepository) GetByID(ctx context.Context, id string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cacheKey := "id:" + id
	if val, ok := r.getFromCacheLocked(cacheKey); ok {
		return val, nil
	}

	var rawJSON string
	err := r.db.QueryRowContext(ctx, QueryGetByID, id).Scan(&rawJSON)
	if err != nil {
		return nil, err
	}

	bytes := []byte(rawJSON)
	r.setToCacheLocked(cacheKey, bytes)
	return bytes, nil
}

func (r *SQLiteRepository) GetByNamed(ctx context.Context, fuzzy string, setCode string) ([]byte, error) {
	clean := cleanName(fuzzy)
	if clean == "" {
		return nil, sql.ErrNoRows
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	cacheKey := fmt.Sprintf("named:%s:%s", clean, setCode)
	if val, ok := r.getFromCacheLocked(cacheKey); ok {
		return val, nil
	}

	var rawJSON string
	var err error

	// 1. Try exact clean name match
	if setCode != "" {
		err = r.db.QueryRowContext(ctx, QueryGetByNamedExactSet, clean, strings.ToLower(setCode)).Scan(&rawJSON)
	} else {
		err = r.db.QueryRowContext(ctx, QueryGetByNamedExact, clean).Scan(&rawJSON)
	}

	// 2. Try prefix match
	if err == sql.ErrNoRows {
		if setCode != "" {
			err = r.db.QueryRowContext(ctx, QueryGetByNamedPrefixSet, clean+"%", strings.ToLower(setCode)).Scan(&rawJSON)
		} else {
			err = r.db.QueryRowContext(ctx, QueryGetByNamedPrefix, clean+"%").Scan(&rawJSON)
		}
	}

	if err != nil {
		return nil, err
	}

	bytes := []byte(rawJSON)
	r.setToCacheLocked(cacheKey, bytes)
	return bytes, nil
}

func (r *SQLiteRepository) GetBySetCol(ctx context.Context, setCode, colNum, lang string) ([]byte, error) {
	setCode = strings.ToLower(setCode)
	lang = strings.ToLower(lang)

	r.mu.RLock()
	defer r.mu.RUnlock()

	cacheKey := fmt.Sprintf("setcol:%s:%s:%s", setCode, colNum, lang)
	if val, ok := r.getFromCacheLocked(cacheKey); ok {
		return val, nil
	}

	var rawJSON string
	// Try specific language first
	err := r.db.QueryRowContext(ctx, QueryGetBySetColLang, setCode, colNum, lang).Scan(&rawJSON)
	
	// Fallback to English or any set/col if missing
	if err == sql.ErrNoRows {
		err = r.db.QueryRowContext(ctx, QueryGetBySetCol, setCode, colNum).Scan(&rawJSON)
	}

	if err != nil {
		return nil, err
	}

	bytes := []byte(rawJSON)
	r.setToCacheLocked(cacheKey, bytes)
	return bytes, nil
}

func (r *SQLiteRepository) GetRandom(ctx context.Context, qParam string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	whereSql, params := parseQuery(qParam)

	var rawJSON string
	if whereSql == "" {
		// Optimization: O(1) single-query random lookup via materialized subquery
		err := r.db.QueryRowContext(ctx, QueryGetRandomNoFilters).Scan(&rawJSON)
		if err == nil {
			return []byte(rawJSON), nil
		}
	}

	queryStr := QueryBaseSelect + whereSql + " ORDER BY RANDOM() LIMIT 1"
	err := r.db.QueryRowContext(ctx, queryStr, params...).Scan(&rawJSON)
	if err != nil {
		return nil, err
	}
	return []byte(rawJSON), nil
}

func (r *SQLiteRepository) Search(ctx context.Context, qParam, unique string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	whereSql, params := parseQuery(qParam)
	sqlQuery := QueryBaseSelect + whereSql + " ORDER BY LENGTH(name) ASC LIMIT 100"

	rows, err := r.db.QueryContext(ctx, sqlQuery, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []json.RawMessage
	for rows.Next() {
		var rawJSON string
		if err := rows.Scan(&rawJSON); err != nil {
			return nil, err
		}
		cards = append(cards, json.RawMessage(rawJSON))
	}

	resultMap := map[string]any{
		"object":      "list",
		"total_cards": len(cards),
		"has_more":    false,
		"data":        cards,
	}

	return json.Marshal(resultMap)
}

func (r *SQLiteRepository) getFromCacheLocked(key string) ([]byte, bool) {
	val, ok := r.cache[key]
	return val, ok
}

func (r *SQLiteRepository) setToCacheLocked(key string, val []byte) {
	// Prevent unbounded growth by clearing cache if it gets too large (> 5000 items)
	if len(r.cache) > 5000 {
		r.cache = make(map[string][]byte)
	}
	r.cache[key] = val
}

func cleanName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		// Lowercase on the fly
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func negateOp(op string, negate bool) string {
	if negate {
		if op == "=" {
			return "!="
		}
		return "NOT " + op
	}
	return op
}

// comparisonOp converts a Scryfall comparison operator string into a valid SQL comparison operator.
// Scryfall supports =, !=, <, >, <=, >= as well as the alias ":", which means "=" for exact matches.
func comparisonOp(op string) string {
	switch op {
	case ":", "=":
		return "="
	case "!=":
		return "!="
	case "<":
		return "<"
	case ">":
		return ">"
	case "<=":
		return "<="
	case ">=":
		return ">="
	default:
		return "="
	}
}

func parseQuery(q string) (whereSql string, params []any) {
	uDec, err := url.QueryUnescape(q)
	if err == nil {
		q = uDec
	}

	q = strings.ReplaceAll(q, "+", " ")
	tokens := strings.Fields(q)

	var clauses []string
	for _, token := range tokens {
		negate := false
		if strings.HasPrefix(token, "-") {
			negate = true
			token = token[1:]
		}

		// Try to parse comparison-operator tokens (e.g. cmc>=4, pow<3, ci>=uw)
		// before falling back to the colon-split form.
		parts := strings.SplitN(token, ":", 2)
		var compOp string // set when a comparison operator is found instead of ":"
		if len(parts) != 2 {
			switch {
			case rxColor.MatchString(token):
				m := rxColor.FindStringSubmatch(token)
				parts, compOp = []string{"c", m[2]}, m[1]
			case rxCI.MatchString(token):
				m := rxCI.FindStringSubmatch(token)
				parts, compOp = []string{"ci", m[2]}, m[1]
			case rxCMC.MatchString(token):
				m := rxCMC.FindStringSubmatch(token)
				parts, compOp = []string{m[1], m[3]}, m[2]
			case rxPower.MatchString(token):
				m := rxPower.FindStringSubmatch(token)
				parts, compOp = []string{m[1], m[3]}, m[2]
			case rxTough.MatchString(token):
				m := rxTough.FindStringSubmatch(token)
				parts, compOp = []string{m[1], m[3]}, m[2]
			}
		}

		if len(parts) == 2 {
			key := strings.ToLower(parts[0])
			val := parts[1]
			op := comparisonOp(compOp) // defaults to "=" for colon-split tokens

			switch key {
			case "s", "set":
				clauses = append(clauses, "set_code "+negateOp("=", negate)+" ?")
				params = append(params, strings.ToLower(val))
			case "r", "rarity":
				clauses = append(clauses, "json_extract(raw_json, '$.rarity') "+negateOp("=", negate)+" ?")
				params = append(params, strings.ToLower(val))
			case "t", "type":
				clauses = append(clauses, "json_extract(raw_json, '$.type_line') "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+strings.ToLower(val)+"%")
			case "c", "color":
				clauses = append(clauses, "json_extract(raw_json, '$.colors') "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+strings.ToUpper(val)+"%")
			case "id", "ci", "identity":
				clauses = append(clauses, "json_extract(raw_json, '$.color_identity') "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+strings.ToUpper(val)+"%")
			case "cmc", "mv":
				clauses = append(clauses, "CAST(json_extract(raw_json, '$.cmc') AS REAL) "+negateOp(op, negate)+" ?")
				params = append(params, val)
			case "pow", "power":
				clauses = append(clauses, "json_extract(raw_json, '$.power') "+negateOp(op, negate)+" ?")
				params = append(params, val)
			case "tou", "toughness":
				clauses = append(clauses, "json_extract(raw_json, '$.toughness') "+negateOp(op, negate)+" ?")
				params = append(params, val)
			case "o", "oracle":
				// No B-Tree index — full scan is expected and accepted
				clauses = append(clauses, "json_extract(raw_json, '$.oracle_text') "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+val+"%")
			case "a", "art", "artist":
				// No B-Tree index for infix wildcard — artist index only helps prefix lookups
				clauses = append(clauses, "json_extract(raw_json, '$.artist') "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+val+"%")
			case "lang", "l":
				clauses = append(clauses, "json_extract(raw_json, '$.lang') "+negateOp("=", negate)+" ?")
				params = append(params, strings.ToLower(val))
			case "f", "format", "legal":
				// Legality is stored as a nested JSON object: {"legalities": {"modern": "legal", ...}}
				// json_extract path must be: '$.legalities.modern'
				formatName := strings.ToLower(val)
				jsonPath := fmt.Sprintf("'$.legalities.%s'", formatName)
				if negate {
					clauses = append(clauses, "json_extract(raw_json, "+jsonPath+") != 'legal'")
				} else {
					clauses = append(clauses, "json_extract(raw_json, "+jsonPath+") = 'legal'")
				}
			}
		} else {
			clean := cleanName(token)
			if clean != "" {
				clauses = append(clauses, "name_clean "+negateOp("LIKE", negate)+" ?")
				params = append(params, "%"+clean+"%")
			}
		}
	}

	if len(clauses) > 0 {
		whereSql = " AND " + strings.Join(clauses, " AND ")
	}
	return whereSql, params
}
