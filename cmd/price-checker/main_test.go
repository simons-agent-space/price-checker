package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/simons-agent-space/price-checker/internal/api"
	"github.com/simons-agent-space/price-checker/internal/store"
)

func TestHealthz(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(db))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestRunHealthcheck(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	t.Setenv("DATABASE_URL", "file:"+dbPath+"?_pragma=foreign_keys(1)")

	// Open + migrate so the file exists with the expected schema.
	db, err := sql.Open("sqlite", os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := runHealthcheck(); err != nil {
		t.Errorf("runHealthcheck: unexpected error: %v", err)
	}
}

func TestRunHealthcheckMissingEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	err := runHealthcheck()
	if err == nil {
		t.Fatal("runHealthcheck: expected error for empty DATABASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("runHealthcheck: error = %q, want mention of DATABASE_URL", err)
	}
}

// TestBuildHandlerWithAuth exercises the full HTTP routing the same
// way main.go does. Both internal/api and internal/web register on
// the same top-level mux, so the JSON API must be namespaced under
// /api/ to avoid the GET /searches/{id} collision that would panic
// http.ServeMux at startup. The test verifies:
//
//   - API routes register successfully under /api/...
//   - web routes register successfully (when auth is configured)
//   - no duplicate http.ServeMux route panic occurs
//   - / is available through the web UI
//   - API endpoints are available under /api/...
//
// This is the regression test for the production crash in #2026-08-09:
// once auth env vars were correctly passed into the container, the
// HTML web routes were also registered, and the duplicate
// GET /searches/{id} pattern made the process panic at startup.
func TestBuildHandlerWithAuth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := buildHandler(db, "admin", "secret", logger)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		auth       bool
		wantStatus int
	}{
		{"healthz", "GET", "/healthz", "", false, http.StatusOK},
		{"api create", "POST", "/api/searches", `{"name":"Sony","query":"sony","check_interval":"1h"}`, false, http.StatusCreated},
		{"api list", "GET", "/api/searches", "", false, http.StatusOK},
		{"api get not found", "GET", "/api/searches/999", "", false, http.StatusNotFound},
		{"api delete not found", "DELETE", "/api/searches/999", "", false, http.StatusNotFound},
		{"web root no auth", "GET", "/", "", false, http.StatusUnauthorized},
		{"web root with auth", "GET", "/", "", true, http.StatusOK},
		{"web search auth", "GET", "/searches/1", "", true, http.StatusOK},
		{"web products auth", "GET", "/products/999", "", true, http.StatusNotFound}, // no product with id 999; 404 confirms the route is registered (handler-level, not mux-level)
		{"web deals auth", "GET", "/deals", "", true, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.auth {
				req.SetBasicAuth("admin", "secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s: status = %d, want %d", tt.method, tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestBuildHandlerNoAuth verifies that buildHandler still works when
// web auth is not configured. The web Register is a no-op, so only
// /api/* and /healthz are reachable. / (the web UI root) must NOT
// match anything (so the mux serves its default 404).
func TestBuildHandlerNoAuth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := buildHandler(db, "", "", logger)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	// API still works.
	req := httptest.NewRequest("GET", "/api/searches", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/searches (no auth): status = %d, want 200", rec.Code)
	}

	// Web UI root is not registered → 404 from the bare mux.
	req = httptest.NewRequest("GET", "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET / (no auth): status = %d, want 404", rec.Code)
	}

	// Web UI bad IDs are not registered → 404 from the bare mux.
	req = httptest.NewRequest("GET", "/searches/abc", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /searches/abc (no auth): status = %d, want 404", rec.Code)
	}
}

// TestBuildHandlerAPI404IsJSON verifies that the JSONErrors middleware
// is scoped to /api/. A 404 under /api/ must be rewritten as JSON, while
// a 404 outside /api/ (the bare mux default) stays plain text so the
// web UI is not contaminated by JSON responses.
func TestBuildHandlerAPI404IsJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := buildHandler(db, "admin", "secret", logger)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	// Unknown API path under /api/ — JSONErrors rewrites this as JSON.
	req := httptest.NewRequest("GET", "/api/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/unknown: status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET /api/unknown Content-Type = %q, want application/json", ct)
	}
	var body api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Errorf("GET /api/unknown body not JSON: %v", err)
	}
	if body.Error == "" {
		t.Error("GET /api/unknown missing 'error' field")
	}

	// Path outside /api/ that doesn't match any web route — plain text
	// 404 from the bare mux, NOT rewritten by JSONErrors.
	req = httptest.NewRequest("GET", "/totally-unknown", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /totally-unknown: status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "application/json" {
		t.Errorf("GET /totally-unknown Content-Type = %q, JSONErrors leaked outside /api/", ct)
	}
}

// TestBuildHandlerAllowsWebUIDespiteJSONAPI verifies that the web UI
// is reachable when auth is enabled, even though the JSON API also
// exposes /api/searches/{id}. This is the regression test for the
// production crash: if internal/api and internal/web both registered
// /searches/{id} on the same mux, http.ServeMux would panic at
// startup before any request could be served.
func TestBuildHandlerAllowsWebUIDespiteJSONAPI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := buildHandler(db, "admin", "secret", logger)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}

	// Create a search via the JSON API at /api/searches.
	body := strings.NewReader(`{"name":"Sony","query":"sony","check_interval":"1h"}`)
	req := httptest.NewRequest("POST", "/api/searches", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/searches: status = %d, want 201", rec.Code)
	}
	var created api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Fetch the same search via the JSON API at /api/searches/{id}.
	req = httptest.NewRequest("GET", "/api/searches/"+strconv.FormatInt(created.ID, 10), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/searches/%d: status = %d, want 200", created.ID, rec.Code)
	}

	// Fetch the same search via the web UI at /searches/{id} (basic auth).
	req = httptest.NewRequest("GET", "/searches/"+strconv.FormatInt(created.ID, 10), nil)
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /searches/%d (auth): status = %d, want 200", created.ID, rec.Code)
	}
}
