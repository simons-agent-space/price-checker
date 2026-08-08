package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

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
