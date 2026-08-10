// Package api implements the HTTP API for the price-checker service.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/simons-agent-space/price-checker/internal/store"
)

const maxRequestBodyBytes = 64 * 1024

type CreateSearchRequest struct {
	Name          string `json:"name"`
	Query         string `json:"query"`
	CheckInterval string `json:"check_interval"`
}

type SearchResponse struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Query         string `json:"query"`
	CheckInterval string `json:"check_interval"`
	NextCheckAt   int64  `json:"next_check_at"`
	CreatedAt     int64  `json:"created_at"`
}

type CreateProductRequest struct {
	URL string `json:"url"`
}

type ProductResponse struct {
	ID            int64  `json:"id"`
	SearchID      int64  `json:"search_id"`
	URL           string `json:"url"`
	LastCheckedAt *int64 `json:"last_checked_at,omitempty"`
	CreatedAt     int64  `json:"created_at"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type Server struct {
	store *store.Store
}

func New(s *store.Store) *Server {
	return &Server{store: s}
}

// Register installs the JSON API routes on mux:
//
//	POST   /searches                 create a search
//	GET    /searches                 list all searches
//	GET    /searches/{id}            fetch a single search
//	DELETE /searches/{id}            delete a search
//	POST   /searches/{id}/products   add a product to a search
//	GET    /searches/{id}/products   list products in a search
//	DELETE /products/{id}            delete a product
//
// The package is namespace-agnostic — these routes are not prefixed
// with /api/. The caller is responsible for mounting this mux under
// the public URI prefix (e.g. /api/) so that the JSON API stays
// disjoint from the HTML web UI routes registered by internal/web.
// Both packages own GET /searches/{id}; the caller keeps
// http.ServeMux from panicking on the duplicate pattern by keeping
// the namespaces separate.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /searches", s.createSearch)
	mux.HandleFunc("GET /searches", s.listSearches)
	mux.HandleFunc("GET /searches/{id}", s.getSearch)
	mux.HandleFunc("DELETE /searches/{id}", s.deleteSearch)
	mux.HandleFunc("POST /searches/{id}/products", s.addProduct)
	mux.HandleFunc("GET /searches/{id}/products", s.listProducts)
	mux.HandleFunc("DELETE /products/{id}", s.deleteProduct)
}

func (s *Server) createSearch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req CreateSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusBadRequest, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	query := strings.TrimSpace(req.Query)
	if name == "" || query == "" {
		writeError(w, http.StatusBadRequest, "name and query are required")
		return
	}
	interval, err := time.ParseDuration(req.CheckInterval)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid check_interval")
		return
	}
	if interval < time.Second {
		writeError(w, http.StatusBadRequest, "check_interval must be at least 1 second")
		return
	}
	created, err := s.store.CreateSearch(r.Context(), &store.Search{
		Name:          name,
		Query:         query,
		CheckInterval: interval,
		NextCheckAt:   time.Now().Add(interval),
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "search with this name and query already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create search")
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(created))
}

func (s *Server) listSearches(w http.ResponseWriter, r *http.Request) {
	searches, err := s.store.ListSearches(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list searches")
		return
	}
	resp := make([]SearchResponse, 0, len(searches))
	for _, search := range searches {
		resp = append(resp, toResponse(search))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	search, err := s.store.GetSearch(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "search not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get search")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(search))
}

func (s *Server) deleteSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteSearch(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "search not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete search")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// addProduct attaches a product URL to an existing search. The search must
// exist (404 otherwise); the URL must be non-empty after trim (400 otherwise);
// duplicate (search_id, url) returns 409. v1 does not enforce any retailer or
// URL-shape constraint — discovery is the external system's responsibility.
func (s *Server) addProduct(w http.ResponseWriter, r *http.Request) {
	searchID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusBadRequest, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	// Pre-check the search exists so a missing search → 404 instead of
	// leaking the FK constraint failure as 500. A race (search deleted
	// between check and insert) would still surface as 500 — acceptable.
	if _, err := s.store.GetSearch(r.Context(), searchID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "search not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lookup search")
		return
	}

	created, err := s.store.AddProduct(r.Context(), &store.Product{
		SearchID: searchID,
		URL:      url,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "product with this url already exists in the search")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create product")
		return
	}
	writeJSON(w, http.StatusCreated, toProductResponse(created))
}

// listProducts returns the products belonging to a search. The search must
// exist (404 otherwise); an empty search returns [] (not null).
func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	searchID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if _, err := s.store.GetSearch(r.Context(), searchID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "search not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lookup search")
		return
	}

	products, err := s.store.ListProductsBySearch(r.Context(), searchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list products")
		return
	}
	resp := make([]ProductResponse, 0, len(products))
	for _, p := range products {
		resp = append(resp, toProductResponse(p))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteProduct(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete product")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toResponse(s *store.Search) SearchResponse {
	return SearchResponse{
		ID:            s.ID,
		Name:          s.Name,
		Query:         s.Query,
		CheckInterval: s.CheckInterval.String(),
		NextCheckAt:   s.NextCheckAt.Unix(),
		CreatedAt:     s.CreatedAt.Unix(),
	}
}

func toProductResponse(p *store.Product) ProductResponse {
	var lastChecked *int64
	if p.LastCheckedAt != nil {
		ts := p.LastCheckedAt.Unix()
		lastChecked = &ts
	}
	return ProductResponse{
		ID:            p.ID,
		SearchID:      p.SearchID,
		URL:           p.URL,
		LastCheckedAt: lastChecked,
		CreatedAt:     p.CreatedAt.Unix(),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// bufferedWriter is an http.ResponseWriter that captures the response so the
// middleware can decide whether to rewrite it (e.g. as JSON for 404/405).
type bufferedWriter struct {
	http.ResponseWriter
	headers       http.Header
	status        int
	body          bytes.Buffer
	headerWritten bool
}

func (w *bufferedWriter) Header() http.Header {
	return w.headers
}

func (w *bufferedWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.status = code
	w.headerWritten = true
}

func (w *bufferedWriter) Write(b []byte) (int, error) {
	if !w.headerWritten {
		w.status = http.StatusOK
		w.headerWritten = true
	}
	return w.body.Write(b)
}

// JSONErrors wraps next so that the Go 1.22 ServeMux plain-text 404/405
// responses are rewritten as JSON ErrorResponse bodies. Successful responses
// pass through unchanged.
func JSONErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := &bufferedWriter{
			ResponseWriter: w,
			headers:        make(http.Header),
		}
		next.ServeHTTP(buf, r)
		if buf.status == 0 {
			buf.status = http.StatusOK
		}
		if buf.status == http.StatusNotFound || buf.status == http.StatusMethodNotAllowed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(buf.status)
			_ = json.NewEncoder(w).Encode(ErrorResponse{Error: http.StatusText(buf.status)})
			return
		}
		for k, v := range buf.headers {
			w.Header()[k] = v
		}
		w.WriteHeader(buf.status)
		_, _ = w.Write(buf.body.Bytes())
	})
}
