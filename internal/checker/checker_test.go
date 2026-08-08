package checker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simons-agent-space/price-checker/internal/notifier"
	"github.com/simons-agent-space/price-checker/internal/store"
)

// silentLogger returns a slog.Logger that discards output. Tests use it so
// the test runner stays quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Recording handler captures every log record so tests can assert that a
// deal was detected (or not). Test-only.
type recordingHandler struct {
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) hasMsg(msg string) bool {
	for _, r := range h.records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

// --- parser tests ---

func TestParsePrice_EuroSymbolDot(t *testing.T) {
	body := []byte(`<html><span class="price">€12.99</span></html>`)
	cents, currency, err := ParsePrice(body)
	if err != nil {
		t.Fatal(err)
	}
	if cents != 1299 {
		t.Errorf("cents = %d, want 1299", cents)
	}
	if currency != "EUR" {
		t.Errorf("currency = %q, want EUR", currency)
	}
}

func TestParsePrice_EuroSymbolComma(t *testing.T) {
	body := []byte(`<html>Preis: 12,99 €</html>`)
	cents, _, err := ParsePrice(body)
	if err != nil {
		t.Fatal(err)
	}
	if cents != 1299 {
		t.Errorf("cents = %d, want 1299", cents)
	}
}

func TestParsePrice_EURCodeThousandSeparator(t *testing.T) {
	// 1.234,56 doesn't match the regex (thousand separator); the regex
	// picks up nothing in this body, so we expect ErrPriceNotFound.
	_, _, err := ParsePrice([]byte(`<html>EUR 1.234,56</html>`))
	if !errors.Is(err, ErrPriceNotFound) {
		t.Errorf("err = %v, want ErrPriceNotFound (no match for thousand-separator format)", err)
	}
}

func TestParsePrice_EURCodeNoSeparator(t *testing.T) {
	body := []byte(`<html>EUR 12.99</html>`)
	cents, currency, err := ParsePrice(body)
	if err != nil {
		t.Fatal(err)
	}
	if cents != 1299 {
		t.Errorf("cents = %d, want 1299", cents)
	}
	if currency != "EUR" {
		t.Errorf("currency = %q, want EUR", currency)
	}
}

func TestParsePrice_NoPrice(t *testing.T) {
	body := []byte(`<html><title>No price here</title></html>`)
	_, _, err := ParsePrice(body)
	if !errors.Is(err, ErrPriceNotFound) {
		t.Errorf("err = %v, want ErrPriceNotFound", err)
	}
}

func TestParsePrice_Empty(t *testing.T) {
	_, _, err := ParsePrice(nil)
	if !errors.Is(err, ErrPriceNotFound) {
		t.Errorf("err = %v, want ErrPriceNotFound", err)
	}
}

// --- deal detector tests ---

func TestIsDeal_BelowMinBaseline(t *testing.T) {
	// 1 entry is below MinBaseline; never flag a deal.
	isDeal, median := IsDeal([]int64{1000}, 100)
	if isDeal {
		t.Error("isDeal = true, want false (insufficient baseline)")
	}
	if median != 0 {
		t.Errorf("median = %d, want 0", median)
	}
}

func TestIsDeal_AtThreshold(t *testing.T) {
	// median = 1000, threshold = 20% → cutoff = 800. 800 is NOT < 800.
	// Use 799 to trigger the deal.
	prices := []int64{1000, 1000, 1000}
	isDeal, median := IsDeal(prices, 799)
	if !isDeal {
		t.Errorf("isDeal = false, want true (price 799 < cutoff 800); median = %d", median)
	}
	if median != 1000 {
		t.Errorf("median = %d, want 1000", median)
	}
}

func TestIsDeal_NotBelowThreshold(t *testing.T) {
	prices := []int64{1000, 1000, 1000}
	isDeal, _ := IsDeal(prices, 850)
	if isDeal {
		t.Error("isDeal = true, want false (850 above cutoff 800)")
	}
}

func TestIsDeal_MedianComputation(t *testing.T) {
	// 5 entries: 100, 200, 300, 400, 500 → median = 300.
	prices := []int64{100, 200, 300, 400, 500}
	// cutoff = 240 → 200 is a deal
	if isDeal, median := IsDeal(prices, 200); !isDeal || median != 300 {
		t.Errorf("isDeal=%v median=%d, want true/300", isDeal, median)
	}
	// cutoff = 240 → 241 is not a deal
	if isDeal, _ := IsDeal(prices, 241); isDeal {
		t.Error("isDeal = true, want false (241 above cutoff 240)")
	}
}

// --- checker integration tests ---

// pageServer returns an httptest.Server whose /product/:id handler serves
// the supplied body. Use it to drive the checker end-to-end.
func pageServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
}

func setupSearchProduct(t *testing.T, url string) (*store.Store, *store.Search, *store.Product) {
	t.Helper()
	st := store.NewTestStore(t)
	ctx := context.Background()
	search, err := st.CreateSearch(ctx, &store.Search{
		Name:          "test",
		Query:         "anything",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	productID, err := st.AddProduct(ctx, &store.Product{
		SearchID: search.ID,
		URL:      url,
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := st.GetProduct(ctx, productID)
	if err != nil {
		t.Fatal(err)
	}
	return st, search, product
}

func TestChecker_SuccessRecordsPriceCheck(t *testing.T) {
	page := pageServer(t, `<html><span class="price">€12.99</span></html>`)
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	c := New(st, notifier.Noop{}, silentLogger())

	if err := c.Check(context.Background(), search); err != nil {
		t.Fatalf("Check: %v", err)
	}

	recent, err := st.ListRecentPriceChecks(context.Background(), product.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("price_checks = %d, want 1", len(recent))
	}
	got := recent[0]
	if !got.Success {
		t.Error("Success = false, want true")
	}
	if got.PriceCents != 1299 {
		t.Errorf("PriceCents = %d, want 1299", got.PriceCents)
	}
	if got.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", got.Currency)
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty", got.Error)
	}

	updated, err := st.GetProduct(context.Background(), product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckedAt == nil {
		t.Error("LastCheckedAt = nil, want non-nil after successful check")
	}
}

func TestChecker_FetchFailureRecordsFailedCheck(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	c := New(st, notifier.Noop{}, silentLogger())

	if err := c.Check(context.Background(), search); err != nil {
		t.Fatalf("Check: %v", err)
	}
	// A failed check must record a price_checks row (so the dashboard
	// can show "last error") and stamp the product's last_checked_at.
	count, err := st.CountPriceChecks(context.Background(), product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("price_checks count = %d, want 1", count)
	}
	updated, err := st.GetProduct(context.Background(), product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckedAt == nil {
		t.Error("LastCheckedAt = nil, want non-nil after failed check")
	}
}

func TestChecker_ParseFailureRecordsFailedCheck(t *testing.T) {
	page := pageServer(t, `<html>no price here</html>`)
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	c := New(st, notifier.Noop{}, silentLogger())

	if err := c.Check(context.Background(), search); err != nil {
		t.Fatalf("Check: %v", err)
	}

	updated, err := st.GetProduct(context.Background(), product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastCheckedAt == nil {
		t.Error("LastCheckedAt = nil, want non-nil after parse failure")
	}
}

func TestChecker_DealDetected(t *testing.T) {
	// Page returns €1.00 (100 cents). Baseline is 1000 cents. With
	// 4 entries [1000, 1000, 1000, 100], median = 1000, cutoff = 800.
	// 100 < 800 → deal.
	page := pageServer(t, `<html><span class="price">€1.00</span></html>`)
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	ctx := context.Background()

	for range 3 {
		if _, err := st.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  product.ID,
			PriceCents: 1000,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recordingHandler{}
	logger := slog.New(rec)
	c := New(st, notifier.Noop{}, logger)

	if err := c.Check(ctx, search); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !rec.hasMsg("deal detected") {
		t.Errorf("expected 'deal detected' log, got: %v", rec.records)
	}
}

func TestChecker_NoDealAboveThreshold(t *testing.T) {
	page := pageServer(t, `<html><span class="price">€9.99</span></html>`)
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	ctx := context.Background()

	// Baseline: 4 checks at 1000. Current = 999. Cutoff = 800. Not a deal.
	for range 4 {
		if _, err := st.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  product.ID,
			PriceCents: 1000,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recordingHandler{}
	c := New(st, notifier.Noop{}, slog.New(rec))

	if err := c.Check(ctx, search); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if rec.hasMsg("deal detected") {
		t.Error("deal detected, want none (999 is above cutoff 800)")
	}
}

func TestChecker_MultipleProducts(t *testing.T) {
	cheap := pageServer(t, `<html><span class="price">€1.00</span></html>`)
	defer cheap.Close()
	expensive := pageServer(t, `<html><span class="price">€999.99</span></html>`)
	defer expensive.Close()

	st := store.NewTestStore(t)
	ctx := context.Background()
	search, err := st.CreateSearch(ctx, &store.Search{
		Name:          "multi",
		Query:         "q",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{cheap.URL, expensive.URL} {
		if _, err := st.AddProduct(ctx, &store.Product{
			SearchID: search.ID,
			URL:      url,
		}); err != nil {
			t.Fatal(err)
		}
	}

	c := New(st, notifier.Noop{}, silentLogger())
	if err := c.Check(ctx, search); err != nil {
		t.Fatalf("Check: %v", err)
	}

	products, err := st.ListProductsBySearch(ctx, search.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 2 {
		t.Fatalf("products = %d, want 2", len(products))
	}
	for _, p := range products {
		recent, err := st.ListRecentPriceChecks(ctx, p.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(recent) != 1 {
			t.Errorf("product %d: price_checks = %d, want 1", p.ID, len(recent))
		}
	}
}

func TestChecker_NilStorePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil store")
		}
	}()
	_ = New(nil, notifier.Noop{}, silentLogger())
}

func TestChecker_NilLoggerFallsBack(t *testing.T) {
	c := New(store.NewTestStore(t), notifier.Noop{}, nil)
	if c.logger == nil {
		t.Error("logger is nil, want fallback")
	}
}

func TestChecker_CtxCancelStopsIteration(t *testing.T) {
	// Set up a search with two products. The first check observes
	// ctx.Err() and returns; the second is never checked.
	var urls []string
	for range 2 {
		srv := pageServer(t, `<html><span class="price">€5.00</span></html>`)
		defer srv.Close()
		urls = append(urls, srv.URL)
	}

	ctx, cancel := context.WithCancel(context.Background())
	st := store.NewTestStore(t)
	search, err := st.CreateSearch(ctx, &store.Search{
		Name:          "x",
		Query:         "q",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if _, err := st.AddProduct(ctx, &store.Product{SearchID: search.ID, URL: u}); err != nil {
			t.Fatal(err)
		}
	}

	c := New(st, notifier.Noop{}, silentLogger())
	// Cancel after the first check returns from checkProduct; the
	// second iteration's ctx.Err() check returns the error.
	cancel()
	err = c.Check(ctx, search)
	if err == nil {
		t.Fatalf("Check returned nil err; expected context.Canceled (or an error containing it)")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestNewFetcher_HasUAAndTimeout(t *testing.T) {
	f := NewFetcher()
	if f.ua == "" {
		t.Error("ua = empty, want default")
	}
	if f.client.Timeout == 0 {
		t.Error("timeout = 0, want > 0")
	}
}

func TestFetcher_FollowsGetAndParsesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent header missing")
		}
		fmt.Fprint(w, "€42.00")
	}))
	defer srv.Close()

	body, err := NewFetcher().Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "€42.00") {
		t.Errorf("body = %q, want €42.00", string(body))
	}
}

func TestFetcher_NonOKReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewFetcher().Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want mention of 404", err)
	}
}

// --- notifier integration ---

// recordingNotifier captures every Notify call so tests can assert that a
// deal reached the notifier and inspect the payload. It can also be
// configured to return an error to test the checker's error handling.
type recordingNotifier struct {
	calls []notifier.Deal
	err   error
}

func (r *recordingNotifier) Notify(_ context.Context, d notifier.Deal) error {
	if r.err != nil {
		return r.err
	}
	r.calls = append(r.calls, d)
	return nil
}

func TestChecker_DealTriggersNotifier(t *testing.T) {
	// Page returns €1.00 (100 cents). Baseline of 3 checks at 1000 cents
	// gives median 1000, cutoff 800. 100 < 800 → deal → Notify.
	page := pageServer(t, `<html><span class="price">€1.00</span></html>`)
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	ctx := context.Background()

	for range 3 {
		if _, err := st.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  product.ID,
			PriceCents: 1000,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	rn := &recordingNotifier{}
	c := New(st, rn, silentLogger())

	if err := c.Check(ctx, search); err != nil {
		t.Fatalf("Check: %v", err)
	}

	if len(rn.calls) != 1 {
		t.Fatalf("notifier calls = %d, want 1", len(rn.calls))
	}
	got := rn.calls[0]
	if got.ProductID != product.ID {
		t.Errorf("ProductID = %d, want %d", got.ProductID, product.ID)
	}
	if got.ProductURL != product.URL {
		t.Errorf("ProductURL = %q, want %q", got.ProductURL, product.URL)
	}
	if got.PriceCents != 100 {
		t.Errorf("PriceCents = %d, want 100", got.PriceCents)
	}
	if got.MedianCents != 1000 {
		t.Errorf("MedianCents = %d, want 1000", got.MedianCents)
	}
	if got.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", got.Currency)
	}
}

func TestChecker_NotifyErrorDoesNotAbortLoop(t *testing.T) {
	// A notifier that returns an error must be logged but must not
	// stop the check loop — price tracking is more important than
	// alerting.
	page := pageServer(t, `<html><span class="price">€1.00</span></html>`)
	defer page.Close()

	st, search, product := setupSearchProduct(t, page.URL)
	ctx := context.Background()

	for range 3 {
		if _, err := st.RecordPriceCheck(ctx, &store.PriceCheck{
			ProductID:  product.ID,
			PriceCents: 1000,
			Currency:   "EUR",
			Success:    true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	rn := &recordingNotifier{err: errors.New("telegram is down")}
	rec := &recordingHandler{}
	c := New(st, rn, slog.New(rec))

	if err := c.Check(ctx, search); err != nil {
		t.Fatalf("Check: %v, want nil (notifier error must not abort)", err)
	}
	if !rec.hasMsg("notify") {
		t.Errorf("expected 'notify' log on notifier error, got: %v", rec.records)
	}
}

func TestChecker_NilNotifierFallsBackToNoop(t *testing.T) {
	// Passing nil for the notifier must be safe — the checker must
	// always see a valid Notifier, so callers don't need a guard.
	c := New(store.NewTestStore(t), nil, silentLogger())
	if c.notifier == nil {
		t.Fatal("notifier is nil, want Noop fallback")
	}
	if _, ok := c.notifier.(notifier.Noop); !ok {
		t.Errorf("notifier = %T, want notifier.Noop", c.notifier)
	}
}
