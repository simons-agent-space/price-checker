// Package main implements the price-checker service.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/simons-agent-space/price-checker/internal/api"
	"github.com/simons-agent-space/price-checker/internal/checker"
	"github.com/simons-agent-space/price-checker/internal/notifier"
	"github.com/simons-agent-space/price-checker/internal/scheduler"
	"github.com/simons-agent-space/price-checker/internal/store"
)

func main() {
	// Compose healthcheck probe. Distroless has no shell, so CMD-SHELL is
	// unavailable in the compose healthcheck. The binary supports its own
	// non-mutating probe instead: ping the DB with a short timeout, exit 0
	// on success, 1 on failure. Runs before migrations so a broken DB
	// surface shows up as an unhealthy container rather than an exit.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		if err := runHealthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("sqlite", os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // ponytail: SQLite WAL serialises writers; 1 conn avoids SQLITE_BUSY

	if err := store.Migrate(db); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}

	st := store.New(db)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":3000"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(db))
	api.New(st).Register(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.JSONErrors(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// SCHEDULER_INTERVAL is operator-facing; invalid input is logged and
	// the default (30s) is used.
	interval := 30 * time.Second
	if v := os.Getenv("SCHEDULER_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			slog.Warn("invalid SCHEDULER_INTERVAL, using default", "value", v, "err", err)
		} else {
			interval = d
		}
	}

	// Checker fans each due search out to its products: fetch the URL,
	// parse the price, record a price_check, detect deals. The scheduler
	// bounds each call to interval.
	// Notifier: Telegram when both TELEGRAM_BOT_TOKEN and
	// TELEGRAM_CHAT_ID are set, Noop otherwise. The checker always
	// sees a valid Notifier, so missing creds are not a startup error.
	var n notifier.Notifier = notifier.Noop{}
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	switch {
	case token != "" && chatID != "":
		n = notifier.NewTelegram(token, chatID)
		slog.Info("telegram notifier enabled")
	case token != "" || chatID != "":
		// Exactly one set: silent fallback would make the operator
		// think the notifier is wired when it is not. Warn loudly.
		slog.Warn("telegram notifier misconfigured: set both TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID, using Noop")
	}
	ch := checker.New(st, n, slog.Default())
	sched := scheduler.New(st, ch.Check, interval, slog.Default())
	slog.Info("starting scheduler", "interval", interval)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown http", "err", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		sched.Run(ctx)
	}()

	slog.Info("starting price-checker", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}

	wg.Wait()
}

func healthz(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, `{"status":"db unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// runHealthcheck is the CLI probe used by the compose healthcheck. It opens
// the SQLite database from DATABASE_URL, pings it with a short timeout, and
// returns a non-nil error on any failure. It does not migrate or write.
func runHealthcheck() error {
	dbPath := os.Getenv("DATABASE_URL")
	if dbPath == "" {
		return fmt.Errorf("DATABASE_URL not set")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}
	return nil
}
