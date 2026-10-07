package api

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/store"
)

// bcryptCost is a var so tests can lower it.
var bcryptCost = 12

// dummyHash makes logins for unknown emails take as long as real ones,
// so response time doesn't reveal which emails have accounts.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("bookly-dummy-password"), bcryptCost)
	return h
})

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,48}[a-z0-9])$`)
	priceRe = regexp.MustCompile(`^\d{1,8}(\.\d{1,2})?$`)

	reservedSlugs = map[string]bool{"api": true, "admin": true, "owner": true, "login": true,
		"signup": true, "health": true, "static": true, "assets": true, "www": true}
)

func invalid(msg string) error { return &booking.ValidationError{Msg: msg} }

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("invalid id")
	}
	return id, nil
}

func cleanName(s, what string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", invalid(what + " is required")
	}
	if utf8.RuneCountInString(s) > 100 {
		return "", invalid(what + " is too long (max 100 characters)")
	}
	return s, nil
}

func validTimezone(tz string) error {
	if tz == "" || tz == "Local" {
		return invalid("timezone is required, e.g. Europe/London")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return invalid("unknown timezone " + strconv.Quote(tz))
	}
	return nil
}

func cleanEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 255 {
		return "", invalid("please enter a valid email address")
	}
	return s, nil
}

// Auth

type signupRequest struct {
	BusinessName string `json:"business_name"`
	Slug         string `json:"slug"`
	Timezone     string `json:"timezone"`
	Email        string `json:"email"`
	Password     string `json:"password"`
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var in signupRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	name, err := cleanName(in.BusinessName, "business name")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(slug) || strings.Contains(slug, "--") || reservedSlugs[slug] {
		handleErr(w, r, invalid("booking link must be 3–50 lowercase letters, numbers or dashes"))
		return
	}
	if err := validTimezone(in.Timezone); err != nil {
		handleErr(w, r, err)
		return
	}
	email, err := cleanEmail(in.Email)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := validPassword(in.Password); err != nil {
		handleErr(w, r, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcryptCost)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	b, err := s.store.CreateBusiness(r.Context(), store.NewBusiness{
		Name: name, Slug: slug, Timezone: in.Timezone, OwnerEmail: email, PasswordHash: hash,
		TrialEndsAt: s.now().AddDate(0, 0, s.cfg.TrialDays),
	})
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.startSession(w, r.Context(), b.ID); err != nil {
		handleErr(w, r, err)
		return
	}
	_, token, err := s.store.CreateEmailVerification(r.Context(), b.ID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	s.sendEmail(b.OwnerEmail, "Welcome to "+s.cfg.CompanyName+" – please confirm your email", fmt.Sprintf(
		"Hi,\n\n%s is set up. Please confirm this is your email address:\n%s/verify/%s\n\n"+
			"Your booking link is:\n%s/b/%s\n\n"+
			"Next: add your services and opening hours at %s/owner\n",
		b.Name, s.cfg.PublicURL, token, s.cfg.PublicURL, b.Slug, s.cfg.PublicURL))
	writeJSON(w, http.StatusCreated, b)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	id, hash, err := s.store.Credentials(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
	if err != nil {
		hash = dummyHash()
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) != nil || err != nil {
		writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	if err := s.startSession(w, r.Context(), id); err != nil {
		handleErr(w, r, err)
		return
	}
	b, err := s.store.BusinessByID(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) startSession(w http.ResponseWriter, ctx context.Context, businessID int64) error {
	token, err := s.store.CreateSession(ctx, businessID)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token, store.SessionTTL)
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), c.Value); err != nil {
			handleErr(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Business settings

func (s *Server) getBusiness(w http.ResponseWriter, r *http.Request) {
	s.getBusinessByID(w, r, businessID(r))
}

func (s *Server) getBusinessByID(w http.ResponseWriter, r *http.Request, id int64) {
	b, err := s.store.BusinessByID(r.Context(), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

type businessUpdate struct {
	Name            string `json:"name"`
	Timezone        string `json:"timezone"`
	GoogleReviewURL string `json:"google_review_url"`
	Currency        string `json:"currency"`
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Server) updateBusiness(w http.ResponseWriter, r *http.Request) {
	var in businessUpdate
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	name, err := cleanName(in.Name, "business name")
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := validTimezone(in.Timezone); err != nil {
		handleErr(w, r, err)
		return
	}
	review := strings.TrimSpace(in.GoogleReviewURL)
	if review != "" {
		u, err := url.Parse(review)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(review) > 500 {
			handleErr(w, r, invalid("review link must be an https:// URL"))
			return
		}
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" { // not sent: keep the current one
		cur, err := s.store.BusinessByID(r.Context(), businessID(r))
		if err != nil {
			handleErr(w, r, err)
			return
		}
		currency = cur.Currency
	}
	if !currencyRe.MatchString(currency) {
		handleErr(w, r, invalid("currency must be a 3-letter code like GBP, EUR, USD or INR"))
		return
	}
	b, err := s.store.UpdateBusiness(r.Context(), businessID(r), name, in.Timezone, review, currency)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// Services

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	svcs, err := s.store.Services(r.Context(), businessID(r), false)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, svcs)
}

func validService(sv *store.Service) error {
	name, err := cleanName(sv.Name, "service name")
	if err != nil {
		return err
	}
	sv.Name = name
	if sv.DurationMin < 5 || sv.DurationMin > 12*60 {
		return invalid("duration must be between 5 and 720 minutes")
	}
	sv.Price = strings.TrimSpace(sv.Price)
	if sv.Price == "" {
		sv.Price = "0"
	}
	if !priceRe.MatchString(sv.Price) {
		return invalid("price must be a number like 25 or 25.50")
	}
	return nil
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	sv := store.Service{Active: true}
	if err := decode(w, r, &sv); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := validService(&sv); err != nil {
		handleErr(w, r, err)
		return
	}
	sv, err := s.store.CreateService(r.Context(), businessID(r), sv)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, sv)
}

func (s *Server) updateService(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var sv store.Service
	if err := decode(w, r, &sv); err != nil {
		handleErr(w, r, err)
		return
	}
	sv.ID = id
	if err := validService(&sv); err != nil {
		handleErr(w, r, err)
		return
	}
	sv, err = s.store.UpdateService(r.Context(), businessID(r), sv)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sv)
}

// Opening hours

func (s *Server) getHours(w http.ResponseWriter, r *http.Request) {
	hours, err := s.store.OpeningHours(r.Context(), businessID(r))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toHoursJSON(hours))
}

func (s *Server) setHours(w http.ResponseWriter, r *http.Request) {
	var in []hoursJSON
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	seen := map[int]bool{}
	hours := make([]booking.OpeningHours, 0, len(in))
	for _, h := range in {
		if h.Weekday < 0 || h.Weekday > 6 || seen[h.Weekday] {
			handleErr(w, r, invalid("each weekday (0–6) may appear once"))
			return
		}
		seen[h.Weekday] = true
		opens, err := booking.ParseClock(h.Opens)
		if err != nil {
			handleErr(w, r, err)
			return
		}
		closes, err := booking.ParseClock(h.Closes)
		if err != nil {
			handleErr(w, r, err)
			return
		}
		if closes <= opens {
			handleErr(w, r, invalid("closing time must be after opening time"))
			return
		}
		hours = append(hours, booking.OpeningHours{Weekday: time.Weekday(h.Weekday), Opens: opens, Closes: closes})
	}
	if err := s.store.SetOpeningHours(r.Context(), businessID(r), hours); err != nil {
		handleErr(w, r, err)
		return
	}
	s.getHours(w, r)
}

// Appointments

func (s *Server) businessLocation(r *http.Request) (*time.Location, error) {
	b, err := s.store.BusinessByID(r.Context(), businessID(r))
	if err != nil {
		return nil, err
	}
	return time.LoadLocation(b.Timezone)
}

// listAppointments returns appointments for ?date=YYYY-MM-DD (business
// local time), or for ?days=N days starting at that date (max 31).
func (s *Server) listAppointments(w http.ResponseWriter, r *http.Request) {
	loc, err := s.businessLocation(r)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	date := booking.DateOf(s.now(), loc)
	if q := r.URL.Query().Get("date"); q != "" {
		if date, err = booking.ParseDate(q); err != nil {
			handleErr(w, r, err)
			return
		}
	}
	days := 1
	if q := r.URL.Query().Get("days"); q != "" {
		days, err = strconv.Atoi(q)
		if err != nil || days < 1 || days > 31 {
			handleErr(w, r, invalid("days must be between 1 and 31"))
			return
		}
	}
	from := date.At(0, loc)
	end := booking.Date{Year: date.Year, Month: date.Month, Day: date.Day + days}
	to := booking.DateOf(end.At(12*60, loc), loc).At(0, loc) // normalises day overflow
	appts, err := s.store.AppointmentsBetween(r.Context(), businessID(r), from, to)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": date.String(), "appointments": appts})
}

type statusRequest struct {
	Status booking.Status `json:"status"`
}

func (s *Server) setAppointmentStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var in statusRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	switch in.Status {
	case booking.StatusConfirmed, booking.StatusCancelled, booking.StatusDone, booking.StatusNoShow:
	default:
		handleErr(w, r, invalid("status must be confirmed, cancelled, done or no_show"))
		return
	}
	a, err := s.store.SetStatus(r.Context(), businessID(r), id, in.Status, s.now())
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	msgs, err := s.store.Messages(r.Context(), businessID(r), id)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	st, err := s.store.Stats(r.Context(), businessID(r), now.AddDate(0, 0, -90), now)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": 90, "stats": st})
}
