// Package config loads settings from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	DatabaseURL    string
	Port           string
	WorkerInterval time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        getenv("PORT", "8080"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (see .env.example)")
	}

	interval, err := time.ParseDuration(getenv("WORKER_INTERVAL", "30s"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid WORKER_INTERVAL: %w", err)
	}
	cfg.WorkerInterval = interval

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
