// Package api is the HTTP layer: routing, JSON, auth and error mapping.
// Business rules live in internal/booking, SQL in internal/store.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/config"
	"github.com/YoshrithMalhotra/bookly/internal/store"
)

type Server struct {
	store *store.Store
	cfg   config.Config
	now   func() time.Time

	bookingLimit *rateLimiter
	authLimit    *rateLimiter
}

func New(s *store.Store, cfg config.Config) *Server {
	return &Server{
		store:        s,
		cfg:          cfg,
		now:          time.Now,
		bookingLimit: newRateLimiter(10, time.Minute), // per IP
		authLimit:    newRateLimiter(10, time.Minute), // per IP, login + signup
	}
}

// Handler returns the full HTTP handler with middleware applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/health", s.health)

	// Public: customers booking and managing their appointment.
	mux.HandleFunc("GET /api/businesses/{slug}", s.getPublicBusiness)
	mux.HandleFunc("GET /api/businesses/{slug}/slots", s.getSlots)
	mux.Handle("POST /api/businesses/{slug}/appointments", s.limit(s.bookingLimit, s.createAppointment))
	mux.HandleFunc("GET /api/manage/{token}", s.getManagedAppointment)
	mux.HandleFunc("POST /api/manage/{token}/cancel", s.cancelManagedAppointment)

	// Owner auth.
	mux.Handle("POST /api/signup", s.limit(s.authLimit, s.signup))
	mux.Handle("POST /api/login", s.limit(s.authLimit, s.login))
	mux.HandleFunc("POST /api/logout", s.logout)

	// Owner: every handler is scoped to the logged-in owner's business.
	mux.Handle("GET /api/owner/business", s.auth(s.getBusiness))
	mux.Handle("PUT /api/owner/business", s.auth(s.updateBusiness))
	mux.Handle("GET /api/owner/services", s.auth(s.listServices))
	mux.Handle("POST /api/owner/services", s.auth(s.createService))
	mux.Handle("PUT /api/owner/services/{id}", s.auth(s.updateService))
	mux.Handle("GET /api/owner/hours", s.auth(s.getHours))
	mux.Handle("PUT /api/owner/hours", s.auth(s.setHours))
	mux.Handle("GET /api/owner/appointments", s.auth(s.listAppointments))
	mux.Handle("POST /api/owner/appointments/{id}/status", s.auth(s.setAppointmentStatus))
	mux.Handle("GET /api/owner/appointments/{id}/messages", s.auth(s.listMessages))
	mux.Handle("GET /api/owner/stats", s.auth(s.getStats))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	if s.cfg.WebDir != "" {
		mux.Handle("GET /", spa(s.cfg.WebDir))
	}

	return s.recoverer(s.logRequests(s.securityHeaders(s.csrf(mux))))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		slog.Error("health: db ping", "error", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("OK"))
}

// JSON helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError sends every error in the same shape: {"error": "..."}.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

const maxBody = 64 << 10

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &booking.ValidationError{Msg: "invalid JSON body: " + err.Error()}
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return &booking.ValidationError{Msg: "invalid JSON body: trailing data"}
	}
	return nil
}

// handleErr maps domain errors to HTTP status codes.
func handleErr(w http.ResponseWriter, r *http.Request, err error) {
	var ve *booking.ValidationError
	var ce *store.ConflictError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Msg)
	case errors.Is(err, booking.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, booking.ErrSlotTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, &ce):
		writeError(w, http.StatusConflict, ce.Error())
	case errors.Is(err, context.Canceled):
		// client went away; nothing to send
	default:
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong, please try again")
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}
