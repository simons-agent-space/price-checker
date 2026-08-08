package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simons-agent-space/price-checker/internal/store"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setupServer returns a web Server backed by a real (temp-file) SQLite
// store, with a search and two products pre-seeded.
func setupServer(t *testing.T) (*Server, *store.Search, *store.Product, *store.Product) {
	t.Helper()
	st := store.NewTestStore(t)
	ctx := context.Background()
	search, err := st.CreateSearch(ctx, &store.Search{
		Name:          "test-search",
		Query:         "anything",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	p1ID, err := st.AddProduct(ctx, &store.Product{SearchID: search.ID, URL: "https://example.com/p/1"})
	if err != nil {
		t.Fatal(err)
	}
	p2ID, err := st.AddProduct(ctx, &store.Product{SearchID: search.ID, URL: "https://example.com/p/2"})
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := st.GetProduct(ctx, p1ID)
	p2, _ := st.GetProduct(ctx, p2ID)
	s, err := New(st, "admin", "secret", silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	return s, search, p1, p2
}

func newMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

func TestServer_RequiresAuth(t *testing.T) {
	s, _, _, _ := setupServer(t)
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// No creds → 401.
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want Basic", got)
	}

	// Wrong creds → 401.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.SetBasicAuth("admin", "wrong")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for wrong creds", resp.StatusCode)
	}

	// Right creds → 200.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for right creds", resp.StatusCode)
	}
}

func TestServer_DisabledWithoutCreds(t *testing.T) {
	st := store.NewTestStore(t)
	// No creds: Register must not add any routes.
	s, err := New(st, "", "", silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// No creds AND no creds configured → 404 (mux falls through).
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (web UI disabled)", resp.StatusCode)
	}
}

func TestServer_Index(t *testing.T) {
	s, search, _, _ := setupServer(t)
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(body), search.Name) {
		t.Errorf("body missing search name %q", search.Name)
	}
	if !strings.Contains(string(body), "2") {
		t.Errorf("body missing product count 2")
	}
}

func TestServer_Search(t *testing.T) {
	s, search, p1, p2 := setupServer(t)
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/searches/"+strconv.FormatInt(search.ID, 10), nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{p1.URL, p2.URL} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body missing URL %q", want)
		}
	}
}

func TestServer_Product(t *testing.T) {
	s, _, p1, _ := setupServer(t)
	ctx := context.Background()
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID:  p1.ID,
		PriceCents: 1999,
		Currency:   "EUR",
		Success:    true,
	}); err != nil {
		t.Fatal(err)
	}
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/products/"+strconv.FormatInt(p1.ID, 10), nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), p1.URL) {
		t.Errorf("body missing URL %q", p1.URL)
	}
	if !strings.Contains(string(body), "€19.99") {
		t.Errorf("body missing formatted price €19.99")
	}
}

func TestServer_Deals(t *testing.T) {
	s, search, p1, _ := setupServer(t)
	ctx := context.Background()
	// Seed 3 baseline checks at 1000c, then a 100c check → deal.
	for range 3 {
		if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  p1.ID,
			PriceCents: 1000,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID:  p1.ID,
		PriceCents: 100,
		Currency:   "EUR",
		Success:    true,
	}); err != nil {
		t.Fatal(err)
	}
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/deals", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), p1.URL) {
		t.Errorf("body missing deal URL %q", p1.URL)
	}
	if !strings.Contains(string(body), search.Name) {
		t.Errorf("body missing search name %q", search.Name)
	}
	if !strings.Contains(string(body), "€1.00") {
		t.Errorf("body missing current price €1.00")
	}
	if !strings.Contains(string(body), "€10.00") {
		t.Errorf("body missing median price €10.00")
	}
}

func TestServer_NotFound(t *testing.T) {
	s, _, _, _ := setupServer(t)
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/nonexistent", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServer_BadIDs(t *testing.T) {
	s, _, _, _ := setupServer(t)
	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/searches/abc", "/products/xyz"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.SetBasicAuth("admin", "secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestServer_DealsBaselineExcludesCurrent pins the bug where the deals
// page passed the whole recent slice (including the current price) to
// IsDeal. With the fix the baseline is recent[1:], so the current price
// does not bias the median against itself.
func TestServer_DealsBaselineExcludesCurrent(t *testing.T) {
	s, search, p1, _ := setupServer(t)
	ctx := context.Background()

	// Build a baseline that has a real median we can assert on.
	// 4 checks at 1000c, then 1 at 100c → deal, median must be 1000.
	for range 4 {
		if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  p1.ID,
			PriceCents: 1000,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID:  p1.ID,
		PriceCents: 100,
		Currency:   "EUR",
		Success:    true,
	}); err != nil {
		t.Fatal(err)
	}

	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/deals", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), p1.URL) {
		t.Errorf("body missing deal URL %q\nbody: %s", p1.URL, string(body))
	}
	if !strings.Contains(string(body), search.Name) {
		t.Errorf("body missing search name %q", search.Name)
	}
	if !strings.Contains(string(body), "€1.00") {
		t.Errorf("body missing current price €1.00")
	}
	// Median must be the baseline (1000c = €10.00), not the current
	// price — if the median were 100c the row would say €1.00 twice.
	if !strings.Contains(string(body), "€10.00") {
		t.Errorf("body missing median price €10.00 (baseline must exclude current)")
	}
}

// TestServer_DealsIgnoresMixedBaseline reproduces the sub-agent's
// scenario: a baseline of [100..500, 1500..5000] (median 1500) plus a
// current price of 700. With the fix the deal is detected (700 < 1200).
// Without it the median would be 700 and the deal would be missed.
func TestServer_DealsIgnoresMixedBaseline(t *testing.T) {
	s, _, p1, _ := setupServer(t)
	ctx := context.Background()
	prices := []int64{100, 200, 300, 400, 500, 1500, 2000, 3000, 4000, 5000}
	for _, p := range prices {
		if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  p1.ID,
			PriceCents: p,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID:  p1.ID,
		PriceCents: 700,
		Currency:   "EUR",
		Success:    true,
	}); err != nil {
		t.Fatal(err)
	}

	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/deals", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), p1.URL) {
		t.Errorf("body missing deal URL %q (deal should be detected)", p1.URL)
	}
}

// TestServer_FailedCheckShowsFailedStatus verifies that a product whose
// most recent check failed is rendered as "failed" (not "ok", not "—")
// in the search list. The bug was that productSummary used the
// successful-only ListRecentPriceChecks, so failed checks were hidden.
func TestServer_FailedCheckShowsFailedStatus(t *testing.T) {
	s, search, p1, _ := setupServer(t)
	ctx := context.Background()
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID: p1.ID,
		Success:   false,
		Error:     "fetch timed out",
	}); err != nil {
		t.Fatal(err)
	}

	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/searches/"+strconv.FormatInt(search.ID, 10), nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), p1.URL) {
		t.Errorf("body missing product URL %q", p1.URL)
	}
	if !strings.Contains(string(body), "failed") {
		t.Errorf("body missing 'failed' status (failed check hidden by successful-only query)")
	}
	if strings.Contains(string(body), ">ok<") {
		t.Errorf("body shows 'ok' for a failed check")
	}
}

// TestServer_ProductHistoryShowsFailedChecks verifies the product
// detail page renders failed checks with the failure reason.
func TestServer_ProductHistoryShowsFailedChecks(t *testing.T) {
	s, _, p1, _ := setupServer(t)
	ctx := context.Background()
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID:  p1.ID,
		Success:    true,
		PriceCents: 1000,
		Currency:   "EUR",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.RecordPriceCheck(ctx, &store.PriceCheck{
		ProductID: p1.ID,
		Success:   false,
		Error:     "5xx from origin",
	}); err != nil {
		t.Fatal(err)
	}

	mux := newMux(s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/products/"+strconv.FormatInt(p1.ID, 10), nil)
	req.SetBasicAuth("admin", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "5xx from origin") {
		t.Errorf("body missing failure reason")
	}
	if !strings.Contains(string(body), "€10.00") {
		t.Errorf("body missing the successful check's price")
	}
}
