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
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/simons-agent-space/price-checker/internal/api"
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

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "err", err)
		}
	}()

	slog.Info("starting price-checker", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}
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
