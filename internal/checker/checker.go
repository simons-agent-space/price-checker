// Package checker runs the per-search price check loop. The scheduler
// hands each due search to Check, which fans out to every product of that
// search: fetch the URL, parse the price, record a price_check row, and
// detect deals. Failures are recorded as failed price_checks; one bad
// product must not stall the rest of the search.
package checker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/simons-agent-space/price-checker/internal/notifier"
	"github.com/simons-agent-space/price-checker/internal/store"
)

// Checker is the scheduler's CheckFunc. It owns the fetcher; the store is
// the system-wide store. The notifier receives every detected deal; a nil
// notifier is replaced with notifier.Noop so callers can pass nil safely.
type Checker struct {
	store    *store.Store
	fetcher  *Fetcher
	notifier notifier.Notifier
	logger   *slog.Logger
}

// New returns a Checker with the default fetcher and the given logger
// (nil → slog.Default). The store must be non-nil. A nil notifier is
// replaced with notifier.Noop so the checker is always wired to a valid
// Notifier.
func New(st *store.Store, n notifier.Notifier, logger *slog.Logger) *Checker {
	if st == nil {
		panic("checker: nil store")
	}
	if n == nil {
		n = notifier.Noop{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{
		store:    st,
		fetcher:  NewFetcher(),
		notifier: n,
		logger:   logger,
	}
}

// Check implements scheduler.CheckFunc. It iterates each product of the
// search, fetches its URL, parses the price, records a price_check, and
// detects deals. Ctx cancellation stops iteration. Errors are logged and
// recorded as failed checks; the function returns nil unless ctx is
// cancelled (the scheduler treats a non-nil return as a fatal loop error).
func (c *Checker) Check(ctx context.Context, s *store.Search) error {
	products, err := c.store.ListProductsBySearch(ctx, s.ID)
	if err != nil {
		return fmt.Errorf("list products: %w", err)
	}

	for _, p := range products {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.checkProduct(ctx, p)
	}
	return nil
}

// checkProduct runs one product through the full pipeline. Failures at
// any stage are logged and recorded as a failed price_checks row. The
// product's last_checked_at is stamped even on failure so the dashboard
// can show "checked recently".
func (c *Checker) checkProduct(ctx context.Context, p *store.Product) {
	body, err := c.fetcher.Fetch(ctx, p.URL)
	now := time.Now()
	if err != nil {
		c.recordFailure(ctx, p.ID, now, err)
		return
	}
	priceCents, currency, err := ParsePrice(body)
	if err != nil {
		c.recordFailure(ctx, p.ID, now, fmt.Errorf("parse: %w", err))
		return
	}

	// Read the baseline BEFORE recording the current check. Otherwise
	// the deal detector compares the current price against a median
	// that includes itself, which suppresses legitimate deals when the
	// current price falls inside the baseline cluster.
	recent, err := c.store.ListRecentPriceChecks(ctx, p.ID, Window)
	if err != nil {
		c.logger.Error("list recent checks",
			"product_id", p.ID, "err", err)
		// Fall through: record the check but skip deal detection.
		recent = nil
	}

	check := &store.PriceCheck{
		ProductID:  p.ID,
		PriceCents: priceCents,
		Currency:   currency,
		Success:    true,
	}
	if _, err := c.store.RecordPriceCheck(ctx, check); err != nil {
		c.logger.Error("record price check",
			"product_id", p.ID, "err", err)
		return
	}
	if err := c.store.UpdateProductCheckedAt(ctx, p.ID, now); err != nil {
		c.logger.Error("update product last_checked_at",
			"product_id", p.ID, "err", err)
	}

	prices := make([]int64, len(recent))
	for i, r := range recent {
		prices[i] = r.PriceCents
	}
	if deal, median := IsDeal(prices, priceCents); deal {
		c.logger.Info("deal detected",
			"product_id", p.ID,
			"price_cents", priceCents,
			"median_cents", median,
		)
		// Notify failures are logged but do not abort the check loop.
		// A flaky notifier must not stall price tracking for the rest
		// of the search.
		if err := c.notifier.Notify(ctx, notifier.Deal{
			ProductID:   p.ID,
			ProductURL:  p.URL,
			PriceCents:  priceCents,
			MedianCents: median,
			Currency:    currency,
		}); err != nil {
			c.logger.Error("notify",
				"product_id", p.ID, "err", err)
		}
	}
}

func (c *Checker) recordFailure(ctx context.Context, productID int64, now time.Time, err error) {
	c.logger.Warn("check failed",
		"product_id", productID, "err", err)
	check := &store.PriceCheck{
		ProductID: productID,
		Success:   false,
		Error:     err.Error(),
	}
	if _, recErr := c.store.RecordPriceCheck(ctx, check); recErr != nil {
		c.logger.Error("record failure",
			"product_id", productID, "err", recErr)
	}
	if uErr := c.store.UpdateProductCheckedAt(ctx, productID, now); uErr != nil {
		c.logger.Error("update product last_checked_at on failure",
			"product_id", productID, "err", uErr)
	}
}
