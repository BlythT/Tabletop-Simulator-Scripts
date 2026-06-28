package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	_ "github.com/glebarez/go-sqlite"
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
}

type SQLiteRepository struct {
	db    *sql.DB
	cache map[string][]byte
	mu    sync.RWMutex
}

func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &SQLiteRepository{
		db:    db,
		cache: make(map[string][]byte),
	}, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

func (r *SQLiteRepository) Init(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS cards (
			id TEXT PRIMARY KEY,
			name TEXT,
			name_clean TEXT,
			set_code TEXT,
			collector_number TEXT,
			lang TEXT,
			raw_json TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_cards_name_clean ON cards(name_clean);
		CREATE INDEX IF NOT EXISTS idx_cards_set_col ON cards(set_code, collector_number);
	`)
	return err
}

func (r *SQLiteRepository) SaveBatch(ctx context.Context, cards []IngestionCard) error {
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
	cacheKey := "id:" + id
	if val, ok := r.getFromCache(cacheKey); ok {
		return val, nil
	}

	var rawJSON string
	err := r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE id = ? LIMIT 1", id).Scan(&rawJSON)
	if err != nil {
		return nil, err
	}

	bytes := []byte(rawJSON)
	r.setToCache(cacheKey, bytes)
	return bytes, nil
}

func (r *SQLiteRepository) GetByNamed(ctx context.Context, fuzzy string, setCode string) ([]byte, error) {
	clean := cleanName(fuzzy)
	if clean == "" {
		return nil, sql.ErrNoRows
	}

	cacheKey := fmt.Sprintf("named:%s:%s", clean, setCode)
	if val, ok := r.getFromCache(cacheKey); ok {
		return val, nil
	}

	var rawJSON string
	var err error

	// 1. Try exact clean name match
	if setCode != "" {
		err = r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE name_clean = ? AND set_code = ? LIMIT 1", clean, strings.ToLower(setCode)).Scan(&rawJSON)
	} else {
		err = r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE name_clean = ? LIMIT 1", clean).Scan(&rawJSON)
	}

	// 2. Try prefix match
	if err == sql.ErrNoRows {
		if setCode != "" {
			err = r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE name_clean LIKE ? AND set_code = ? LIMIT 1", clean+"%", strings.ToLower(setCode)).Scan(&rawJSON)
		} else {
			err = r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE name_clean LIKE ? LIMIT 1", clean+"%").Scan(&rawJSON)
		}
	}

	if err != nil {
		return nil, err
	}

	bytes := []byte(rawJSON)
	r.setToCache(cacheKey, bytes)
	return bytes, nil
}

func (r *SQLiteRepository) GetBySetCol(ctx context.Context, setCode, colNum, lang string) ([]byte, error) {
	setCode = strings.ToLower(setCode)
	lang = strings.ToLower(lang)

	cacheKey := fmt.Sprintf("setcol:%s:%s:%s", setCode, colNum, lang)
	if val, ok := r.getFromCache(cacheKey); ok {
		return val, nil
	}

	var rawJSON string
	// Try specific language first
	err := r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE set_code = ? AND collector_number = ? AND lang = ? LIMIT 1", setCode, colNum, lang).Scan(&rawJSON)
	
	// Fallback to English or any set/col if missing
	if err == sql.ErrNoRows {
		err = r.db.QueryRowContext(ctx, "SELECT raw_json FROM cards WHERE set_code = ? AND collector_number = ? LIMIT 1", setCode, colNum).Scan(&rawJSON)
	}

	if err != nil {
		return nil, err
	}

	bytes := []byte(rawJSON)
	r.setToCache(cacheKey, bytes)
	return bytes, nil
}

func (r *SQLiteRepository) GetRandom(ctx context.Context, qParam string) ([]byte, error) {
	whereSql, params := parseQuery(qParam)

	var rawJSON string
	queryStr := "SELECT raw_json FROM cards WHERE 1=1" + whereSql + " ORDER BY RANDOM() LIMIT 1"

	err := r.db.QueryRowContext(ctx, queryStr, params...).Scan(&rawJSON)
	if err != nil {
		return nil, err
	}
	return []byte(rawJSON), nil
}

func (r *SQLiteRepository) Search(ctx context.Context, qParam, unique string) ([]byte, error) {
	// Parse basic filters from Scryfall q parameter (e.g. set:xxx, t:xxx)
	var setCode, typeLine string
	
	// Extract set:xxx
	if m := regexp.MustCompile(`(?i)set:(\w+)`).FindStringSubmatch(qParam); len(m) > 1 {
		setCode = strings.ToLower(m[1])
		qParam = strings.ReplaceAll(qParam, m[0], "")
	}
	
	// Extract t:xxx
	if m := regexp.MustCompile(`(?i)t:(\w+)`).FindStringSubmatch(qParam); len(m) > 1 {
		typeLine = strings.ToLower(m[1])
		qParam = strings.ReplaceAll(qParam, m[0], "")
	}

	searchName := cleanName(qParam)

	sqlQuery := "SELECT raw_json FROM cards WHERE 1=1"
	var params []any

	if setCode != "" {
		sqlQuery += " AND set_code = ?"
		params = append(params, setCode)
	}
	if typeLine != "" {
		sqlQuery += " AND raw_json LIKE ?"
		params = append(params, `%"type_line":"%`+typeLine+`%"`)
	}
	if searchName != "" {
		sqlQuery += " AND name_clean LIKE ?"
		params = append(params, "%"+searchName+"%")
	}

	sqlQuery += " ORDER BY LENGTH(name) ASC LIMIT 100"

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

func (r *SQLiteRepository) getFromCache(key string) ([]byte, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	val, ok := r.cache[key]
	return val, ok
}

func (r *SQLiteRepository) setToCache(key string, val []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Prevent unbounded growth by clearing cache if it gets too large (> 5000 items)
	if len(r.cache) > 5000 {
		r.cache = make(map[string][]byte)
	}
	r.cache[key] = val
}

func cleanName(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
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

		parts := strings.SplitN(token, ":", 2)
		if len(parts) != 2 {
			if m := regexp.MustCompile(`(?i)^c([=<>]+)(.+)$`).FindStringSubmatch(token); len(m) > 2 {
				parts = []string{"c", m[2]}
			}
		}

		if len(parts) == 2 {
			key := strings.ToLower(parts[0])
			val := parts[1]

			switch key {
			case "s", "set":
				if negate {
					clauses = append(clauses, "set_code != ?")
				} else {
					clauses = append(clauses, "set_code = ?")
				}
				params = append(params, strings.ToLower(val))
			case "r", "rarity":
				if negate {
					clauses = append(clauses, "raw_json NOT LIKE ?")
				} else {
					clauses = append(clauses, "raw_json LIKE ?")
				}
				params = append(params, fmt.Sprintf(`%%"rarity":"%s"%%`, strings.ToLower(val)))
			case "t", "type":
				if negate {
					clauses = append(clauses, "raw_json NOT LIKE ?")
				} else {
					clauses = append(clauses, "raw_json LIKE ?")
				}
				params = append(params, fmt.Sprintf(`%%"type_line":"%%%s%%"%%`, strings.ToLower(val)))
			case "id", "c", "color":
				if negate {
					clauses = append(clauses, "raw_json NOT LIKE ?")
				} else {
					clauses = append(clauses, "raw_json LIKE ?")
				}
				params = append(params, fmt.Sprintf(`%%"%s"%%`, strings.ToUpper(val)))
			}
		} else {
			clean := cleanName(token)
			if clean != "" {
				if negate {
					clauses = append(clauses, "name_clean NOT LIKE ?")
				} else {
					clauses = append(clauses, "name_clean LIKE ?")
				}
				params = append(params, "%"+clean+"%")
			}
		}
	}

	if len(clauses) > 0 {
		whereSql = " AND " + strings.Join(clauses, " AND ")
	}
	return whereSql, params
}
