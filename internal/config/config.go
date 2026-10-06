// Package config loads settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env            string // "dev" or "prod"
	DatabaseURL    string
	Port           string
	WorkerInterval time.Duration

	// PublicURL is where customers reach the site, e.g. https://bookly.example.com.
	// Used in messages ("manage your booking: ...") and the Origin check.
	PublicURL string
	// WebDir holds the built frontend. Empty disables static file serving.
	WebDir string
	// TrustProxy makes rate limiting use X-Forwarded-For. Only enable it
	// behind a proxy that overwrites that header (Fly, Render, nginx...).
	TrustProxy bool
	// CookieSecure marks the session cookie Secure. Defaults to true unless Env is dev.
	CookieSecure bool
	// DefaultCountryCode turns local numbers like 07700 900123 into E.164.
	DefaultCountryCode string

	// Twilio WhatsApp. When AccountSID is empty the fake sender is used.
	TwilioAccountSID         string
	TwilioAuthToken          string
	TwilioWhatsAppFrom       string // e.g. +14155238886
	TwilioReminderContentSID string // approved template for reminders (optional)
	TwilioReviewContentSID   string // approved template for review requests (optional)
}

func Load() (Config, error) {
	cfg := Config{
		Env:                      getenv("APP_ENV", "prod"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		Port:                     getenv("PORT", "8080"),
		PublicURL:                strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"),
		WebDir:                   os.Getenv("WEB_DIR"),
		DefaultCountryCode:       strings.TrimPrefix(os.Getenv("DEFAULT_COUNTRY_CODE"), "+"),
		TwilioAccountSID:         os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioAuthToken:          os.Getenv("TWILIO_AUTH_TOKEN"),
		TwilioWhatsAppFrom:       os.Getenv("TWILIO_WHATSAPP_FROM"),
		TwilioReminderContentSID: os.Getenv("TWILIO_REMINDER_CONTENT_SID"),
		TwilioReviewContentSID:   os.Getenv("TWILIO_REVIEW_CONTENT_SID"),
	}
	if cfg.Env != "dev" && cfg.Env != "prod" {
		return Config{}, fmt.Errorf("APP_ENV must be dev or prod, got %q", cfg.Env)
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (see .env.example)")
	}
	if cfg.Env == "prod" && cfg.PublicURL == "" {
		return Config{}, fmt.Errorf("PUBLIC_URL is required when APP_ENV=prod")
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = "http://localhost:" + cfg.Port
	}

	interval, err := time.ParseDuration(getenv("WORKER_INTERVAL", "30s"))
	if err != nil || interval <= 0 {
		return Config{}, fmt.Errorf("invalid WORKER_INTERVAL %q", os.Getenv("WORKER_INTERVAL"))
	}
	cfg.WorkerInterval = interval

	if cfg.TrustProxy, err = getbool("TRUST_PROXY", false); err != nil {
		return Config{}, err
	}
	if cfg.CookieSecure, err = getbool("COOKIE_SECURE", cfg.Env != "dev"); err != nil {
		return Config{}, err
	}

	for _, c := range cfg.DefaultCountryCode {
		if c < '0' || c > '9' {
			return Config{}, fmt.Errorf("DEFAULT_COUNTRY_CODE must be digits, e.g. 44 or 91")
		}
	}

	if cfg.TwilioAccountSID != "" && (cfg.TwilioAuthToken == "" || cfg.TwilioWhatsAppFrom == "") {
		return Config{}, fmt.Errorf("TWILIO_AUTH_TOKEN and TWILIO_WHATSAPP_FROM are required with TWILIO_ACCOUNT_SID")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getbool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return b, nil
}
