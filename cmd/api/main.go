package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/YoshrithMalhotra/bookly/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	// TODO(week 1): connect to Postgres via internal/store.

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	// TODO(week 1): GET  /businesses/{slug}/slots?date=YYYY-MM-DD
	// TODO(week 1): POST /businesses/{slug}/appointments

	// TODO: server timeouts + graceful shutdown (copy the pattern from Governor's main.go).
	slog.Info("api listening", "port", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		slog.Error("server", "error", err)
		os.Exit(1)
	}
}
