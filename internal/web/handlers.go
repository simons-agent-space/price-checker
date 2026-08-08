package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/simons-agent-space/price-checker/internal/checker"
	"github.com/simons-agent-space/price-checker/internal/store"
)

// indexData is the data passed to the index template.
type indexData struct {
	Title    string
	Searches []searchRow
}

type searchRow struct {
	ID            int64
	Name          string
	Query         string
	CheckInterval string
	NextCheckAt   string
	ProductCount  int
}

// searchData is the data passed to the search detail template.
type searchData struct {
	Title    string
	Search   searchRow
	Products []productRow
}

type productRow struct {
	ID            int64
	URL           string
	LastCheckedAt string
	LatestPrice   string
	LatestCheckOK bool
	LatestError   string
}

// productData is the data passed to the product detail template.
type productData struct {
	Title  string
	Search searchRow
	Prod   productRow
	Checks []checkRow
}

type checkRow struct {
	CheckedAt string
	Price     string
	Success   bool
	Error     string
}

// dealsData is the data passed to the deals template.
type dealsData struct {
	Title string
	Deals []dealRow
}

type dealRow struct {
	ProductID    int64
	SearchName   string
	URL          string
	CurrentPrice string
	MedianPrice  string
	DiscountPct  int
	CheckedAt    string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	searches, err := s.store.ListSearches(r.Context())
	if err != nil {
		s.logger.Error("list searches", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	rows := make([]searchRow, 0, len(searches))
	for _, search := range searches {
		products, err := s.store.ListProductsBySearch(r.Context(), search.ID)
		// A single broken search shouldn't blank the whole index;
		// log and skip, matching handleDeals' per-search tolerance.
		if err != nil {
			s.logger.Error("list products", "search_id", search.ID, "err", err)
			continue
		}
		rows = append(rows, searchRow{
			ID:            search.ID,
			Name:          search.Name,
			Query:         search.Query,
			CheckInterval: search.CheckInterval.String(),
			NextCheckAt:   formatTime(search.NextCheckAt),
			ProductCount:  len(products),
		})
	}
	if err := s.indexTmpl.ExecuteTemplate(w, "layout", indexData{
		Title:    "price-checker",
		Searches: rows,
	}); err != nil {
		s.logger.Error("render index", "err", err)
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	search, err := s.store.GetSearch(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	products, err := s.store.ListProductsBySearch(r.Context(), id)
	if err != nil {
		s.logger.Error("list products", "search_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	rows := make([]productRow, 0, len(products))
	for _, p := range products {
		rows = append(rows, s.productSummary(r.Context(), p))
	}
	if err := s.searchTmpl.ExecuteTemplate(w, "layout", searchData{
		Title: search.Name,
		Search: searchRow{
			ID:            search.ID,
			Name:          search.Name,
			Query:         search.Query,
			CheckInterval: search.CheckInterval.String(),
			NextCheckAt:   formatTime(search.NextCheckAt),
		},
		Products: rows,
	}); err != nil {
		s.logger.Error("render search", "err", err)
	}
}

func (s *Server) handleProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	product, err := s.store.GetProduct(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	search, err := s.store.GetSearch(r.Context(), product.SearchID)
	if err != nil {
		s.logger.Error("get search", "product_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	checks, err := s.store.ListRecentChecks(r.Context(), id, 50)
	if err != nil {
		s.logger.Error("list checks", "product_id", id, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	rows := make([]checkRow, 0, len(checks))
	for _, c := range checks {
		rows = append(rows, checkRow{
			CheckedAt: formatTime(c.CheckedAt),
			Price:     formatPrice(c.PriceCents, c.Currency),
			Success:   c.Success,
			Error:     c.Error,
		})
	}
	if err := s.productTmpl.ExecuteTemplate(w, "layout", productData{
		Title: "product",
		Search: searchRow{
			ID:   search.ID,
			Name: search.Name,
		},
		Prod:   s.productSummary(r.Context(), product),
		Checks: rows,
	}); err != nil {
		s.logger.Error("render product", "err", err)
	}
}

func (s *Server) handleDeals(w http.ResponseWriter, r *http.Request) {
	searches, err := s.store.ListSearches(r.Context())
	if err != nil {
		s.logger.Error("list searches", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	var deals []dealRow
	for _, search := range searches {
		products, err := s.store.ListProductsBySearch(r.Context(), search.ID)
		if err != nil {
			s.logger.Error("list products", "search_id", search.ID, "err", err)
			continue
		}
		for _, p := range products {
			recent, err := s.store.ListRecentChecks(r.Context(), p.ID, checker.Window+1)
			if err != nil {
				s.logger.Error("list checks", "product_id", p.ID, "err", err)
				continue
			}
			// Need at least MinBaseline prior checks plus the current
			// check; the current price must not be in the baseline or
			// the median reflects the price being evaluated.
			if len(recent) < checker.MinBaseline+1 {
				continue
			}
			current := recent[0].PriceCents
			currency := recent[0].Currency
			prices := make([]int64, len(recent)-1)
			for i, c := range recent[1:] {
				prices[i] = c.PriceCents
			}
			isDeal, median := checker.IsDeal(prices, current)
			if !isDeal {
				continue
			}
			discount := int((median - current) * 100 / median)
			deals = append(deals, dealRow{
				ProductID:    p.ID,
				SearchName:   search.Name,
				URL:          p.URL,
				CurrentPrice: formatPrice(current, currency),
				MedianPrice:  formatPrice(median, currency),
				DiscountPct:  discount,
				CheckedAt:    formatTime(recent[0].CheckedAt),
			})
		}
	}
	if err := s.dealsTmpl.ExecuteTemplate(w, "layout", dealsData{
		Title: "deals",
		Deals: deals,
	}); err != nil {
		s.logger.Error("render deals", "err", err)
	}
}

// productSummary returns a productRow with the latest price filled in.
// ErrNotFound ("no check yet") returns a row with only the URL filled
// in; real store errors are logged but still return a partial row so
// one broken product never breaks the list.
func (s *Server) productSummary(ctx context.Context, p *store.Product) productRow {
	row := productRow{
		ID:  p.ID,
		URL: p.URL,
	}
	if p.LastCheckedAt != nil {
		row.LastCheckedAt = formatTime(*p.LastCheckedAt)
	}
	c, err := s.store.GetLastCheck(ctx, p.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && c == nil) {
		return row
	}
	if err != nil {
		s.logger.Error("get last check", "product_id", p.ID, "err", err)
		return row
	}
	row.LatestPrice = formatPrice(c.PriceCents, c.Currency)
	row.LatestCheckOK = c.Success
	row.LatestError = c.Error
	return row
}
