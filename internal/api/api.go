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

type ErrorResponse struct {
	Error string `json:"error"`
}

type Server struct {
	store *store.Store
}

func New(s *store.Store) *Server {
	return &Server{store: s}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /searches", s.createSearch)
	mux.HandleFunc("GET /searches", s.listSearches)
	mux.HandleFunc("GET /searches/{id}", s.getSearch)
	mux.HandleFunc("DELETE /searches/{id}", s.deleteSearch)
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
