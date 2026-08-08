package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/simons-agent-space/price-checker/internal/store"
)

// silentLogger discards output so test runs stay quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeChecker records calls and returns a configured error. Single-
// goroutine use only — RunOnce calls check synchronously.
type fakeChecker struct {
	calls []*store.Search
	err   error
}

func (c *fakeChecker) check(ctx context.Context, s *store.Search) error {
	c.calls = append(c.calls, s)
	return c.err
}

func TestRunOnce_OnlyDueSearchesAreChecked(t *testing.T) {
	st := store.NewTestStore(t)
	checker := &fakeChecker{}
	ctx := context.Background()

	due, err := st.CreateSearch(ctx, &store.Search{
		Name:          "due",
		Query:         "q1",
		CheckInterval: 24 * time.Hour,
		NextCheckAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	later, err := st.CreateSearch(ctx, &store.Search{
		Name:          "later",
		Query:         "q2",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(st, checker.check, time.Minute, silentLogger())
	before := time.Now()
	s.runOnce(ctx)
	after := time.Now()

	if len(checker.calls) != 1 {
		t.Fatalf("checker calls = %d, want 1", len(checker.calls))
	}
	if checker.calls[0].ID != due.ID {
		t.Errorf("checked id = %d, want %d", checker.calls[0].ID, due.ID)
	}

	gotDue, err := st.GetSearch(ctx, due.ID)
	if err != nil {
		t.Fatal(err)
	}
	// SQLite stores next_check_at as Unix seconds, so the read-back
	// value is truncated to second precision. Match the range.
	wantMin := time.Unix(before.Add(24*time.Hour).Unix(), 0)
	wantMax := time.Unix(after.Add(24*time.Hour).Unix(), 0)
	if gotDue.NextCheckAt.Before(wantMin) || gotDue.NextCheckAt.After(wantMax) {
		t.Errorf("due next_check_at = %v, want in [%v, %v]", gotDue.NextCheckAt, wantMin, wantMax)
	}

	gotLater, err := st.GetSearch(ctx, later.ID)
	if err != nil {
		t.Fatal(err)
	}
	// "later" was not rescheduled: its next_check_at should still be
	// roughly an hour past `before`.
	upperBound := before.Add(time.Hour + time.Second)
	if gotLater.NextCheckAt.After(upperBound) {
		t.Errorf("later next_check_at advanced: %v > %v", gotLater.NextCheckAt, upperBound)
	}
}

func TestRunOnce_NoDueSearchesIsNoOp(t *testing.T) {
	st := store.NewTestStore(t)
	checker := &fakeChecker{}
	ctx := context.Background()

	_, err := st.CreateSearch(ctx, &store.Search{
		Name:          "future",
		Query:         "q",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(st, checker.check, time.Minute, silentLogger())
	s.runOnce(ctx)

	if len(checker.calls) != 0 {
		t.Errorf("checker calls = %d, want 0", len(checker.calls))
	}
}

func TestRunOnce_MultipleDueSearchesInOnePass(t *testing.T) {
	st := store.NewTestStore(t)
	checker := &fakeChecker{}
	ctx := context.Background()

	a, err := st.CreateSearch(ctx, &store.Search{
		Name:          "a",
		Query:         "q1",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateSearch(ctx, &store.Search{
		Name:          "b",
		Query:         "q2",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(st, checker.check, time.Minute, silentLogger())
	s.runOnce(ctx)

	if len(checker.calls) != 2 {
		t.Fatalf("checker calls = %d, want 2", len(checker.calls))
	}
	ids := map[int64]bool{checker.calls[0].ID: true, checker.calls[1].ID: true}
	if !ids[a.ID] || !ids[b.ID] {
		t.Errorf("checked ids = %v, want {%d, %d}", ids, a.ID, b.ID)
	}

	for _, id := range []int64{a.ID, b.ID} {
		got, err := st.GetSearch(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.NextCheckAt.Before(time.Now().Add(time.Hour - time.Second)) {
			t.Errorf("search %d not rescheduled: next_check_at = %v", id, got.NextCheckAt)
		}
	}
}

func TestRunOnce_CheckerErrorStillReschedules(t *testing.T) {
	st := store.NewTestStore(t)
	checker := &fakeChecker{err: errors.New("boom")}
	ctx := context.Background()

	created, err := st.CreateSearch(ctx, &store.Search{
		Name:          "broken",
		Query:         "q",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(st, checker.check, time.Minute, silentLogger())
	before := time.Now()
	s.runOnce(ctx)
	after := time.Now()

	if len(checker.calls) != 1 {
		t.Errorf("checker calls = %d, want 1", len(checker.calls))
	}
	got, err := st.GetSearch(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantMin := time.Unix(before.Add(time.Hour).Unix(), 0)
	wantMax := time.Unix(after.Add(time.Hour).Unix(), 0)
	if got.NextCheckAt.Before(wantMin) || got.NextCheckAt.After(wantMax) {
		t.Errorf("next_check_at = %v, want in [%v, %v] (must advance even on error)", got.NextCheckAt, wantMin, wantMax)
	}
}

func TestRunOnce_CtxCancelMidPass(t *testing.T) {
	st := store.NewTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())

	a, err := st.CreateSearch(ctx, &store.Search{
		Name:          "a",
		Query:         "q1",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateSearch(ctx, &store.Search{
		Name:          "b",
		Query:         "q2",
		CheckInterval: time.Hour,
		NextCheckAt:   time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	var calls []*store.Search
	checker := func(ctx context.Context, s *store.Search) error {
		calls = append(calls, s)
		if s.ID == a.ID {
			cancel() // cancel from inside the first check
		}
		return nil
	}

	s := New(st, checker, time.Hour, silentLogger())
	s.runOnce(ctx)

	if len(calls) != 1 {
		t.Errorf("checker calls = %d, want 1", len(calls))
	}
	if len(calls) >= 1 && calls[0].ID != a.ID {
		t.Errorf("first checked id = %d, want %d", calls[0].ID, a.ID)
	}
	// Outer ctx is cancelled after the first check; use a fresh ctx to
	// verify the post-cancel state of b.
	verifyB, err := st.GetSearch(context.Background(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyB.NextCheckAt.Before(time.Now().Add(-30 * time.Second)) {
		t.Errorf("b's next_check_at was advanced to %v (should still be in the past)", verifyB.NextCheckAt)
	}
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	st := store.NewTestStore(t)
	checker := &fakeChecker{}
	s := New(st, checker.check, 50*time.Millisecond, silentLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of cancel")
	}
}

func TestNew_DefaultsIntervalAndLogger(t *testing.T) {
	st := store.NewTestStore(t)
	checker := &fakeChecker{}

	s := New(st, checker.check, 0, nil)
	if s.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", s.interval)
	}
	if s.logger == nil {
		t.Error("logger is nil")
	}
}

func TestNew_RejectsNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil store")
		}
	}()
	_ = New(nil, func(context.Context, *store.Search) error { return nil }, time.Minute, silentLogger())
}

func TestNew_RejectsNilCheck(t *testing.T) {
	st := store.NewTestStore(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil check")
		}
	}()
	_ = New(st, nil, time.Minute, silentLogger())
}
