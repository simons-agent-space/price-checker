// Package api implements the HTTP API for the price-checker service.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/simons-agent-space/price-checker/internal/store"
)

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
	var req CreateSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if interval <= 0 {
		writeError(w, http.StatusBadRequest, "check_interval must be positive")
		return
	}
	created, err := s.store.CreateSearch(r.Context(), &store.Search{
		Name:          name,
		Query:         query,
		CheckInterval: interval,
		NextCheckAt:   time.Now().Add(interval),
	})
	if err != nil {
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
