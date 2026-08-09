// Package scheduler runs the periodic price-check loop.
//
// Run blocks until the context is cancelled. Each tick, it lists searches
// whose next_check_at has passed, hands them to a CheckFunc, and resets
// next_check_at to (top-of-loop now) + check_interval. A single check
// failure does not stall other searches; errors are logged and the loop
// continues. Each Check call is bounded by a derived context with the
// scheduler's interval as deadline, so a slow check cannot block the loop.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/simons-agent-space/price-checker/internal/store"
)

// CheckFunc is the per-search work the scheduler calls. It MUST honour
// its ctx: the scheduler aborts the loop on ctx cancellation and bounds
// each call to its interval. PR #5 replaces the stub closure with the
// real HTTP fetcher.
type CheckFunc func(ctx context.Context, search *store.Search) error

// Scheduler polls the store for due searches and dispatches them to a
// check function. Construct with New; fields are unexported.
type Scheduler struct {
	store    *store.Store
	check    CheckFunc
	interval time.Duration
	logger   *slog.Logger
}

// New returns a Scheduler; see (*Scheduler).Run. A nil store or check
// function panics — both are required. A zero or negative interval is
// treated as 30s. A nil logger falls back to slog.Default().
func New(s *store.Store, check CheckFunc, interval time.Duration, logger *slog.Logger) *Scheduler {
	if s == nil {
		panic("scheduler: nil store")
	}
	if check == nil {
		panic("scheduler: nil check")
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		store:    s,
		check:    check,
		interval: interval,
		logger:   logger,
	}
}

// Run blocks until ctx is cancelled. It first performs a synchronous
// tick (so a service restart catches up on due searches immediately),
// then ticks every interval until ctx.Done. The synchronous tick and
// the first ticker tick can fire back-to-back when the interval is
// shorter than the check duration — interval is a minimum spacing, not
// a fixed cadence.
func (s *Scheduler) Run(ctx context.Context) {
	s.runOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

// runOnce performs a single scheduling pass: *** due searches, run the
// check on each, and update next_check_at to (top-of-loop now) +
// check_interval. Check and update errors are logged; ctx cancellation
// aborts the iteration. Each check is called with a derived context
// bounded by the interval so a slow check cannot starve the loop.
func (s *Scheduler) runOnce(ctx context.Context) {
	now := time.Now()
	due, err := s.store.ListDueSearches(ctx, now)
	if err != nil {
		s.logger.Error("list due searches", "err", err)
		return
	}

	for _, search := range due {
		if err := ctx.Err(); err != nil {
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, s.interval)
		err := s.check(checkCtx, search)
		cancel()
		if err != nil {
			s.logger.Error("check failed",
				"id", search.ID,
				"name", search.Name,
				"err", err,
			)
		}
		next := now.Add(search.CheckInterval)
		if err := s.store.UpdateNextCheck(ctx, search.ID, next); err != nil {
			s.logger.Error("update next check",
				"id", search.ID,
				"err", err,
			)
		}
	}
}
