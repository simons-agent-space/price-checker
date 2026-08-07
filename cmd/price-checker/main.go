// Package main implements the price-checker service.
//
// Status: placeholder. Single /healthz route so the deployment pipeline
// can be exercised end-to-end. The end goal is described in the design
// document — OpenClaw creates product searches, the service polls URLs,
// tracks price history, detects unusually good prices, and sends Telegram
// notifications.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":3000"
	}

	slog.Info("starting price-checker", "addr", addr)
	if err := http.ListenAndServe(addr, newMux()); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
