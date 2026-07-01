package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

var scryfallBaseURL = "https://api.scryfall.com"

type Server struct {
	repo         CardRepository
	port         int
	updateMutex  sync.Mutex
	updateStatus string // "idle", "running", "success: <timestamp>", "failed: <error>"
	mux          *http.ServeMux
}

func NewServer(repo CardRepository, port int) *Server {
	s := &Server{
		repo:         repo,
		port:         port,
		updateStatus: "idle",
	}

	mux := http.NewServeMux()

	// Go 1.22 structured pattern-matched routing
	mux.HandleFunc("GET /cards/named", s.handleNamed)
	mux.HandleFunc("GET /cards/search", s.handleSearch)
	mux.HandleFunc("GET /cards/random", s.handleRandom)
	mux.HandleFunc("GET /cards/{set}/{col}", s.handleSetCol)
	mux.HandleFunc("GET /cards/{set}/{col}/{lang}", s.handleSetCol)
	mux.HandleFunc("GET /cards/{id}", s.handleID)
	mux.HandleFunc("GET /cards/{id}/rulings", s.handleRulingsPassthrough)
	mux.HandleFunc("POST /batch", s.handleBatch)
	
	// Admin endpoints wrapped in local-only loopback middleware
	mux.Handle("POST /admin/update", s.localOnlyMiddleware(http.HandlerFunc(s.handleAdminUpdate)))
	mux.Handle("GET /admin/update/status", s.localOnlyMiddleware(http.HandlerFunc(s.handleAdminUpdateStatus)))

	s.mux = mux
	return s
}

func (s *Server) localOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			s.sendError(w, "Forbidden: Invalid connection origin", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			s.sendError(w, "Forbidden: Admin endpoints are restricted to localhost connections", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Start() error {
	host := "127.0.0.1" // Bind to localhost (127.0.0.1) by default for local-first host security
	if envHost := os.Getenv("HOST"); envHost != "" {
		host = envHost
	}
	addr := fmt.Sprintf("%s:%d", host, s.port)
	fmt.Printf("HTTP Server starting on http://%s\n", addr)
	return http.ListenAndServe(addr, s.corsMiddleware(s.mux))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.corsMiddleware(s.mux).ServeHTTP(w, r)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleNamed(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	fuzzy := query.Get("fuzzy")
	if fuzzy == "" {
		fuzzy = query.Get("exact")
	}
	if fuzzy == "" {
		s.sendError(w, "Missing 'fuzzy' or 'exact' query parameter", http.StatusBadRequest)
		return
	}
	setCode := query.Get("set")

	bytes, err := s.repo.GetByNamed(r.Context(), fuzzy, setCode)
	if err == sql.ErrNoRows {
		s.sendError(w, fmt.Sprintf("Card not found matching query: %s", fuzzy), http.StatusNotFound)
		return
	}
	if err != nil {
		s.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	qParam := query.Get("q")
	if qParam == "" {
		s.sendError(w, "Missing 'q' query parameter", http.StatusBadRequest)
		return
	}
	unique := query.Get("unique")

	bytes, err := s.repo.Search(r.Context(), qParam, unique)
	if err != nil {
		s.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func (s *Server) handleRandom(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	qParam := query.Get("q")
	count := 1
	if countStr := query.Get("count"); countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 {
			count = c
		}
	}

	bytes, err := s.repo.GetRandom(r.Context(), qParam, count)
	if err != nil {
		s.sendError(w, "Could not retrieve random card: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func (s *Server) handleSetCol(w http.ResponseWriter, r *http.Request) {
	setCode := r.PathValue("set")
	colNum := r.PathValue("col")
	lang := r.PathValue("lang")
	if lang == "" {
		lang = "en"
	}

	bytes, err := s.repo.GetBySetCol(r.Context(), setCode, colNum, lang)
	if err == sql.ErrNoRows {
		s.sendError(w, fmt.Sprintf("Card not found matching Set: %s, Col: %s, Lang: %s", setCode, colNum, lang), http.StatusNotFound)
		return
	}
	if err != nil {
		s.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func (s *Server) handleID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Prevent keyword collision
	if id == "search" || id == "named" || id == "random" {
		s.sendError(w, "Endpoint not supported by proxy server", http.StatusNotFound)
		return
	}

	bytes, err := s.repo.GetByID(r.Context(), id)
	if err == sql.ErrNoRows {
		s.sendError(w, fmt.Sprintf("Card not found with ID: %s", id), http.StatusNotFound)
		return
	}
	if err != nil {
		s.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

type BatchRequest struct {
	URLs []string `json:"urls"`
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	var req BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	var batchResults []json.RawMessage
	for _, urlStr := range req.URLs {
		cardBytes, err := s.resolveURL(r.Context(), urlStr)
		if err != nil {
			errObj := map[string]any{
				"object":  "error",
				"code":    "not_found",
				"status":  404,
				"details": fmt.Sprintf("Card not found for: %s", urlStr),
			}
			errBytes, _ := json.Marshal(errObj)
			batchResults = append(batchResults, json.RawMessage(errBytes))
		} else {
			var listObj struct {
				Object string            `json:"object"`
				Data   []json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(cardBytes, &listObj); err == nil && listObj.Object == "list" {
				batchResults = append(batchResults, listObj.Data...)
			} else {
				batchResults = append(batchResults, json.RawMessage(cardBytes))
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(batchResults); err != nil {
		s.sendError(w, err.Error(), http.StatusInternalServerError)
	}
}

type responseRecorder struct {
	header http.Header
	body   []byte
	code   int
}

func (r *responseRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.code = statusCode
}

func (s *Server) resolveURL(ctx context.Context, urlStr string) ([]byte, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	// Always route relative to the localhost server
	u.Scheme = "http"
	u.Host = "localhost"

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	rec := &responseRecorder{code: http.StatusOK}
	s.mux.ServeHTTP(rec, req)

	if rec.code != http.StatusOK {
		var errObj struct {
			Details string `json:"details"`
		}
		if err := json.Unmarshal(rec.body, &errObj); err == nil && errObj.Details != "" {
			return nil, fmt.Errorf("%s", errObj.Details)
		}
		return nil, fmt.Errorf("resolution failed with status %d", rec.code)
	}

	return rec.body, nil
}

func (s *Server) handleRulingsPassthrough(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scryfallURL := fmt.Sprintf("%s/cards/%s/rulings", scryfallBaseURL, id)
	if r.URL.RawQuery != "" {
		scryfallURL += "?" + r.URL.RawQuery
	}

	fmt.Printf("Passthrough rulings request to: %s\n", scryfallURL)

	req, err := http.NewRequestWithContext(r.Context(), "GET", scryfallURL, nil)
	if err != nil {
		s.sendError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.sendError(w, "Failed to connect to Scryfall rulings: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	s.updateMutex.Lock()
	if s.updateStatus == "running" {
		s.updateMutex.Unlock()
		s.sendError(w, "Update already in progress", http.StatusConflict)
		return
	}
	s.updateStatus = "running"
	s.updateMutex.Unlock()

	// Trigger update in a background goroutine
	go func() {
		ctx := context.Background()
		tempDBPath := s.repo.DBPath() + ".tmp"

		err := UpdateDatabase(ctx, tempDBPath)
		s.updateMutex.Lock()
		defer s.updateMutex.Unlock()

		if err != nil {
			s.updateStatus = "failed: " + err.Error()
			fmt.Printf("Background update failed: %v\n", err)
			return
		}

		// Reload database under write lock
		if err := s.repo.Reload(ctx, tempDBPath); err != nil {
			s.updateStatus = "failed to reload: " + err.Error()
			fmt.Printf("Database reload failed: %v\n", err)
			return
		}

		s.updateStatus = "success: " + time.Now().Format(time.RFC3339)
		fmt.Println("Background update and database swap complete!")
	}()

	w.WriteHeader(http.StatusAccepted)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "update started"})
}

func (s *Server) handleAdminUpdateStatus(w http.ResponseWriter, r *http.Request) {
	s.updateMutex.Lock()
	status := s.updateStatus
	s.updateMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func (s *Server) sendError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	errObj := map[string]any{
		"object":  "error",
		"code":    "bad_request",
		"status":  statusCode,
		"details": message,
	}
	json.NewEncoder(w).Encode(errObj)
}
