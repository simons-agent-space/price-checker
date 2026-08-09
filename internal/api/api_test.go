package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/simons-agent-space/price-checker/internal/api"
	"github.com/simons-agent-space/price-checker/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	srv := api.New(store.NewTestStore(t))
	mux := http.NewServeMux()
	srv.Register(mux)
	return api.JSONErrors(mux)
}

// newTestServerWithStore mirrors newTestServer but also returns the
// underlying store, so tests can inspect side effects (e.g. products
// created alongside a search) without going through the API.
func newTestServerWithStore(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st := store.NewTestStore(t)
	srv := api.New(st)
	mux := http.NewServeMux()
	srv.Register(mux)
	return api.JSONErrors(mux), st
}

func TestCreateSearch(t *testing.T) {
	handler := newTestServer(t)

	body := `{"name":"Sony WH-1000XM5","query":"sony wh-1000xm5","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != "Sony WH-1000XM5" {
		t.Errorf("Name = %q, want %q", resp.Name, "Sony WH-1000XM5")
	}
	if resp.Query != "sony wh-1000xm5" {
		t.Errorf("Query = %q, want %q", resp.Query, "sony wh-1000xm5")
	}
	if resp.ID == 0 {
		t.Error("ID = 0, want non-zero")
	}
	if resp.CheckInterval == "" {
		t.Error("CheckInterval = empty, want non-empty")
	}
	if resp.CreatedAt == 0 {
		t.Error("CreatedAt = 0, want non-zero")
	}
}

func TestListSearches(t *testing.T) {
	handler := newTestServer(t)

	// Empty list
	{
		req := httptest.NewRequest(http.MethodGet, "/searches", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("empty list: status = %d, want 200", rec.Code)
		}
		var resp []api.SearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp) != 0 {
			t.Errorf("empty list: len = %d, want 0", len(resp))
		}
	}

	// Add some searches
	for _, name := range []string{"first", "second"} {
		body := `{"name":"` + name + `","query":"test","check_interval":"1h"}`
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %q: status = %d, want 201", name, rec.Code)
		}
	}

	// Non-empty list
	{
		req := httptest.NewRequest(http.MethodGet, "/searches", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("non-empty list: status = %d, want 200", rec.Code)
		}
		var resp []api.SearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp) != 2 {
			t.Errorf("non-empty list: len = %d, want 2", len(resp))
		}
	}
}

func TestGetSearch(t *testing.T) {
	handler := newTestServer(t)

	createBody := `{"name":"test","query":"test","check_interval":"1h"}`
	createReq := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201", createRec.Code)
	}
	var created api.SearchResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/searches/"+strconv.FormatInt(created.ID, 10), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("get: status = %d, want 200", rec.Code)
	}
	var got api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %d, want %d", got.ID, created.ID)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
}

func TestDeleteSearch(t *testing.T) {
	handler := newTestServer(t)

	createBody := `{"name":"test","query":"test","check_interval":"1h"}`
	createReq := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201", createRec.Code)
	}
	var created api.SearchResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Delete
	{
		req := httptest.NewRequest(http.MethodDelete, "/searches/"+strconv.FormatInt(created.ID, 10), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("delete: status = %d, want 204", rec.Code)
		}
	}

	// Verify gone
	{
		req := httptest.NewRequest(http.MethodGet, "/searches/"+strconv.FormatInt(created.ID, 10), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("get after delete: status = %d, want 404", rec.Code)
		}
	}
}

func TestSearchesValidation(t *testing.T) {
	handler := newTestServer(t)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"missing name", "POST", "/searches", `{"query":"x","check_interval":"1h"}`, http.StatusBadRequest},
		{"missing query", "POST", "/searches", `{"name":"x","check_interval":"1h"}`, http.StatusBadRequest},
		{"whitespace name", "POST", "/searches", `{"name":"   ","query":"x","check_interval":"1h"}`, http.StatusBadRequest},
		{"whitespace query", "POST", "/searches", `{"name":"x","query":"  ","check_interval":"1h"}`, http.StatusBadRequest},
		{"invalid interval", "POST", "/searches", `{"name":"x","query":"x","check_interval":"junk"}`, http.StatusBadRequest},
		{"zero interval", "POST", "/searches", `{"name":"x","query":"x","check_interval":"0s"}`, http.StatusBadRequest},
		{"1ns interval", "POST", "/searches", `{"name":"x","query":"x","check_interval":"1ns"}`, http.StatusBadRequest},
		{"negative interval", "POST", "/searches", `{"name":"x","query":"x","check_interval":"-1h"}`, http.StatusBadRequest},
		{"malformed body", "POST", "/searches", `{not json`, http.StatusBadRequest},
		{"get invalid id", "GET", "/searches/abc", "", http.StatusBadRequest},
		{"get negative id", "GET", "/searches/-1", "", http.StatusNotFound},
		{"get not found", "GET", "/searches/999", "", http.StatusNotFound},
		{"delete invalid id", "DELETE", "/searches/abc", "", http.StatusBadRequest},
		{"delete not found", "DELETE", "/searches/999", "", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus >= 400 {
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
				var errResp api.ErrorResponse
				if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
					t.Errorf("invalid JSON error body: %v", err)
				}
				if errResp.Error == "" {
					t.Error("error response missing 'error' field")
				}
			}
		})
	}
}

func TestCreateSearchConflict(t *testing.T) {
	handler := newTestServer(t)

	body := `{"name":"x","query":"y","check_interval":"1h"}`

	// First create
	{
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("first create: status = %d, want 201", rec.Code)
		}
	}

	// Second create with same (name, query) — should be 409
	{
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("duplicate create: status = %d, want 409", rec.Code)
		}
		var errResp api.ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Errorf("invalid JSON error body: %v", err)
		}
		if errResp.Error == "" {
			t.Error("error response missing 'error' field")
		}
	}

	// Different name — should succeed
	{
		body := `{"name":"z","query":"y","check_interval":"1h"}`
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("different name: status = %d, want 201", rec.Code)
		}
	}
}

func TestCreateSearchBodyTooLarge(t *testing.T) {
	handler := newTestServer(t)

	// Build a body just over 64KB
	padding := strings.Repeat("a", 70_000)
	body := `{"name":"` + padding + `","query":"x","check_interval":"1h"}`

	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	var errResp api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Errorf("invalid JSON error body: %v", err)
	}
	if errResp.Error == "" {
		t.Error("error response missing 'error' field")
	}
}

func TestMethodNotAllowedJSON(t *testing.T) {
	handler := newTestServer(t)

	req := httptest.NewRequest("PATCH", "/searches", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH /searches: status = %d, want 405", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
	if body.Error == "" {
		t.Error("error response missing 'error' field")
	}
}

func TestNotFoundJSON(t *testing.T) {
	handler := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
	if body.Error == "" {
		t.Error("error response missing 'error' field")
	}
}

// TestCreateSearchCreatesProduct asserts that POST /api/searches also
// creates a single product whose URL is the trimmed search query and
// whose search_id is the new search. v1: a search is one product.
func TestCreateSearchCreatesProduct(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	body := `{"name":"watch","query":"https://example.com/product","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	var created api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	products, err := st.ListProductsBySearch(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("len(products) = %d, want 1", len(products))
	}
	if products[0].URL != "https://example.com/product" {
		t.Errorf("URL = %q, want %q", products[0].URL, "https://example.com/product")
	}
	if products[0].SearchID != created.ID {
		t.Errorf("SearchID = %d, want %d", products[0].SearchID, created.ID)
	}
}

// TestCreateSearchConflictNoExtraProduct asserts that a duplicate
// (name, query) returns 409 and does not leave a stray product or
// search behind from the failed call.
func TestCreateSearchConflictNoExtraProduct(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	body := `{"name":"watch","query":"https://example.com/product","check_interval":"1h"}`

	// First create — succeeds, creates one product.
	var firstID int64
	{
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("first: status = %d, want 201", rec.Code)
		}
		var created api.SearchResponse
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatal(err)
		}
		firstID = created.ID
	}

	// Second create with same body — 409, no extra product, no orphan search.
	{
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("duplicate: status = %d, want 409", rec.Code)
		}
	}

	all, err := st.ListSearches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("len(searches) = %d, want 1 (no orphan from conflict)", len(all))
	}
	products, err := st.ListProductsBySearch(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Errorf("len(products) = %d, want 1 (no extra from conflict)", len(products))
	}
}

// TestCreateSearchInvalidCreatesNoProduct: an invalid request body
// returns 4xx and creates no search or product. Complements
// TestSearchesValidation by asserting the side effect rather than only
// the response code.
func TestCreateSearchInvalidCreatesNoProduct(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"query":"x","check_interval":"1h"}`},
		{"missing query", `{"name":"x","check_interval":"1h"}`},
		{"invalid interval", `{"name":"x","query":"x","check_interval":"junk"}`},
		{"malformed body", `{not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code < 400 {
				t.Errorf("status = %d, want >= 400", rec.Code)
			}
		})
	}

	all, err := st.ListSearches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("len(searches) = %d, want 0 (no orphan from invalid requests)", len(all))
	}
}
