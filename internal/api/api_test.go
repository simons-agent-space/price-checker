package api_test

import (
	"context"
	"encoding/json"
	"errors"
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

// TestCreateSearchCreatesNoProduct asserts that POST /api/searches does not
// create any product. v1: a search is a logical group; products are added
// separately via POST /api/searches/{id}/products.
func TestCreateSearchCreatesNoProduct(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	body := `{"name":"watch","query":"compact rice cooker under 150 EUR","check_interval":"1h"}`
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
	if len(products) != 0 {
		t.Errorf("len(products) = %d, want 0", len(products))
	}
}

// TestAddProduct attaches a product URL to an existing search and verifies
// the 201 response shape and store state.
func TestAddProduct(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create search: status = %d, want 201", rec.Code)
	}
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	body := `{"url":"https://example.com/product"}`
	req = httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var product api.ProductResponse
	if err := json.NewDecoder(rec.Body).Decode(&product); err != nil {
		t.Fatal(err)
	}
	if product.ID == 0 {
		t.Error("ID = 0, want non-zero")
	}
	if product.SearchID != search.ID {
		t.Errorf("SearchID = %d, want %d", product.SearchID, search.ID)
	}
	if product.URL != "https://example.com/product" {
		t.Errorf("URL = %q, want %q", product.URL, "https://example.com/product")
	}
	if product.CreatedAt == 0 {
		t.Error("CreatedAt = 0, want non-zero")
	}
	if product.LastCheckedAt != nil {
		t.Errorf("LastCheckedAt = %v, want nil for new product", product.LastCheckedAt)
	}

	products, err := st.ListProductsBySearch(ctx, search.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Errorf("len(products) = %d, want 1", len(products))
	}
}

// TestAddProductTrimsURL asserts that surrounding whitespace is stripped
// before storing, so callers can paste URLs without worrying about format.
func TestAddProductTrimsURL(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	body := `{"url":"  https://example.com/product  "}`
	req = httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var product api.ProductResponse
	if err := json.NewDecoder(rec.Body).Decode(&product); err != nil {
		t.Fatal(err)
	}
	if product.URL != "https://example.com/product" {
		t.Errorf("URL = %q, want %q (trimmed)", product.URL, "https://example.com/product")
	}

	products, _ := st.ListProductsBySearch(ctx, search.ID)
	if len(products) > 0 && products[0].URL != "https://example.com/product" {
		t.Errorf("stored URL = %q, want %q (trimmed)", products[0].URL, "https://example.com/product")
	}
}

// TestAddProductEmptyURL: empty, whitespace-only, or missing URL → 400 and
// no product row.
func TestAddProductEmptyURL(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body string
	}{
		{"empty url", `{"url":""}`},
		{"whitespace url", `{"url":"   "}`},
		{"missing url", `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(tc.body))
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
		})
	}

	products, _ := st.ListProductsBySearch(ctx, search.ID)
	if len(products) != 0 {
		t.Errorf("len(products) = %d, want 0 (no product from invalid requests)", len(products))
	}
}

// TestAddProductMissingSearch: POST /searches/999/products → 404, no row.
func TestAddProductMissingSearch(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	body := `{"url":"https://example.com/product"}`
	req := httptest.NewRequest(http.MethodPost, "/searches/999/products", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	var errResp api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Errorf("invalid JSON error body: %v", err)
	}
	if errResp.Error == "" {
		t.Error("error response missing 'error' field")
	}

	products, _ := st.ListProductsBySearch(ctx, 999)
	if len(products) != 0 {
		t.Errorf("len(products) = %d, want 0 (no row from missing search)", len(products))
	}
}

// TestAddProductDuplicate: adding the same URL to the same search twice →
// 409 on the second call, no extra row.
func TestAddProductDuplicate(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	body := `{"url":"https://example.com/product"}`

	// First add → 201.
	req = httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first add: status = %d, want 201", rec.Code)
	}

	// Second add → 409, no extra row.
	req = httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate: status = %d, want 409", rec.Code)
	}

	products, _ := st.ListProductsBySearch(ctx, search.ID)
	if len(products) != 1 {
		t.Errorf("len(products) = %d, want 1 (no extra from duplicate)", len(products))
	}
}

// TestAddProductSameURLDifferentSearch: the schema permits the same URL
// to live in two different searches (the (search_id, url) UNIQUE is scoped
// per search). v1 leaves cross-search dedupe to the discovery system.
func TestAddProductSameURLDifferentSearch(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	for _, name := range []string{"watch1", "watch2"} {
		body := `{"name":"` + name + `","query":"rice cooker","check_interval":"1h"}`
		req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %q: status = %d, want 201", name, rec.Code)
		}
	}
	searches, _ := st.ListSearches(ctx)
	if len(searches) != 2 {
		t.Fatalf("len(searches) = %d, want 2", len(searches))
	}

	body := `{"url":"https://example.com/product"}`
	for _, search := range searches {
		req := httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("add to search %d: status = %d, want 201", search.ID, rec.Code)
		}
	}

	for _, search := range searches {
		products, _ := st.ListProductsBySearch(ctx, search.ID)
		if len(products) != 1 {
			t.Errorf("search %d: len(products) = %d, want 1", search.ID, len(products))
		}
	}
}

// TestListProducts: GET /searches/{id}/products returns the products.
func TestListProducts(t *testing.T) {
	handler := newTestServer(t)

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	for _, url := range []string{"https://a.example/p", "https://b.example/p"} {
		body := `{"url":"` + url + `"}`
		req := httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("add %q: status = %d, want 201", url, rec.Code)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var products []api.ProductResponse
	if err := json.NewDecoder(rec.Body).Decode(&products); err != nil {
		t.Fatal(err)
	}
	if len(products) != 2 {
		t.Errorf("len = %d, want 2", len(products))
	}
	seen := map[string]bool{}
	for _, p := range products {
		seen[p.URL] = true
	}
	if !seen["https://a.example/p"] || !seen["https://b.example/p"] {
		t.Errorf("missing URL in response: %+v", products)
	}
}

// TestListProductsEmpty: an empty search returns [], not null.
func TestListProductsEmpty(t *testing.T) {
	handler := newTestServer(t)

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

// TestListProductsMissingSearch: GET /searches/999/products → 404.
func TestListProductsMissingSearch(t *testing.T) {
	handler := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/searches/999/products", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestDeleteProduct: DELETE /products/{id} → 204 and the row is gone.
func TestDeleteProduct(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	body := `{"url":"https://example.com/product"}`
	req = httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var product api.ProductResponse
	if err := json.NewDecoder(rec.Body).Decode(&product); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/products/"+strconv.FormatInt(product.ID, 10), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete: status = %d, want 204", rec.Code)
	}

	if _, err := st.GetProduct(ctx, product.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetProduct after delete: err = %v, want ErrNotFound", err)
	}
}

// TestDeleteProductMissing: DELETE /products/999 → 404.
func TestDeleteProductMissing(t *testing.T) {
	handler := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/products/999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestDeleteSearchCascadesProducts: deleting a search removes its products
// (ON DELETE CASCADE on products.search_id). The store layer already proves
// the FK; this guards the API path.
func TestDeleteSearchCascadesProducts(t *testing.T) {
	handler, st := newTestServerWithStore(t)
	ctx := context.Background()

	createBody := `{"name":"watch","query":"rice cooker","check_interval":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/searches", strings.NewReader(createBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var search api.SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&search); err != nil {
		t.Fatal(err)
	}

	body := `{"url":"https://example.com/product"}`
	req = httptest.NewRequest(http.MethodPost, "/searches/"+strconv.FormatInt(search.ID, 10)+"/products", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var product api.ProductResponse
	if err := json.NewDecoder(rec.Body).Decode(&product); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/searches/"+strconv.FormatInt(search.ID, 10), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete search: status = %d, want 204", rec.Code)
	}

	if _, err := st.GetProduct(ctx, product.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("product should be cascade-deleted, got err = %v", err)
	}
}
