// Package web implements the HTML web UI for the price-checker service.
// The UI is neo-brutalist, server-rendered, and protected by basic auth
// when HTTP_BASIC_AUTH_USER and HTTP_BASIC_AUTH_PASS are both set.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/simons-agent-space/price-checker/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

// parsePage returns a template set containing the shared layout plus the
// named page. Each page must define a "content" block; the layout
// includes it via {{template "content" .}}. Parsing per page avoids the
// "last definition wins" trap of a single shared template set.
func parsePage(page string) (*template.Template, error) {
	return template.New("").ParseFS(templatesFS,
		"templates/layout.html",
		"templates/"+page,
	)
}

// Server is the web UI server. When user and pass are both non-empty,
// all registered routes require basic auth. When either is empty, the
// web routes are not registered at all (the UI is disabled and the
// JSON API at /api/searches etc. remains available).
type Server struct {
	store       *store.Store
	user        string
	pass        string
	logger      *slog.Logger
	indexTmpl   *template.Template
	searchTmpl  *template.Template
	productTmpl *template.Template
	dealsTmpl   *template.Template
}

// New returns a web Server. user and pass are the basic auth credentials;
// when either is empty, Register is a no-op and the UI is disabled.
func New(st *store.Store, user, pass string, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	indexTmpl, err := parsePage("index.html")
	if err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	searchTmpl, err := parsePage("search.html")
	if err != nil {
		return nil, fmt.Errorf("parse search: %w", err)
	}
	productTmpl, err := parsePage("product.html")
	if err != nil {
		return nil, fmt.Errorf("parse product: %w", err)
	}
	dealsTmpl, err := parsePage("deals.html")
	if err != nil {
		return nil, fmt.Errorf("parse deals: %w", err)
	}
	return &Server{
		store:       st,
		user:        user,
		pass:        pass,
		logger:      logger,
		indexTmpl:   indexTmpl,
		searchTmpl:  searchTmpl,
		productTmpl: productTmpl,
		dealsTmpl:   dealsTmpl,
	}, nil
}

// Register adds the web routes to mux:
//
//	GET /              index (list of searches)
//	GET /searches/{id} products in a search, with latest price
//	GET /products/{id} price history for a product
//	GET /deals         list of products whose latest check is a deal
//
// All routes require basic auth when the server was configured with
// credentials. When no credentials were set, Register does nothing.
func (s *Server) Register(mux *http.ServeMux) {
	if s.user == "" || s.pass == "" {
		return
	}
	mux.Handle("GET /{$}", s.requireAuth(http.HandlerFunc(s.handleIndex)))
	mux.Handle("GET /searches/{id}", s.requireAuth(http.HandlerFunc(s.handleSearch)))
	mux.Handle("GET /products/{id}", s.requireAuth(http.HandlerFunc(s.handleProduct)))
	mux.Handle("GET /deals", s.requireAuth(http.HandlerFunc(s.handleDeals)))
}

// requireAuth wraps next with basic auth. Requests without valid
// credentials get 401 + WWW-Authenticate.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != s.user || p != s.pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="price-checker"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// formatTime renders a time as RFC3339 in UTC, or "—" for zero values.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

// currencySymbols maps ISO 4217 codes to their display symbol. "" is
// treated as EUR for backwards compatibility with callers that haven't
// been updated to pass the currency explicitly.
var currencySymbols = map[string]string{
	"EUR": "€",
	"USD": "$",
	"JPY": "¥",
	"KRW": "₩",
	"VND": "₫",
	"CLP": "$",
	"ISK": "kr",
}

// zeroDecimalCurrencies have no minor unit; formatPrice renders them
// without a decimal point. The store's currency column is freeform, so
// the list is checked at render time rather than at parse time.
var zeroDecimalCurrencies = map[string]bool{
	"JPY": true,
	"KRW": true,
	"VND": true,
	"CLP": true,
	"ISK": true,
}

// formatPrice renders an integer cents amount as "€12.99" (or the given
// currency). Mirrors notifier.formatPrice but kept local to avoid a
// cross-package import for a one-line helper.
func formatPrice(cents int64, currency string) string {
	if currency == "" {
		currency = "EUR"
	}
	if zeroDecimalCurrencies[currency] {
		sym := currencySymbols[currency]
		if sym == "" {
			sym = currency + " "
		}
		return sym + strconv.FormatInt(cents, 10)
	}
	major := cents / 100
	minor := cents % 100
	if minor < 0 {
		minor = -minor
	}
	sym := currencySymbols[currency]
	if sym == "" {
		sym = currency + " "
	}
	return sym + strconv.FormatInt(major, 10) + "." + fmt.Sprintf("%02d", minor)
}
