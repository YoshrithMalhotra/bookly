package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/store"
)

// Owner-made bookings

type ownerBookingRequest struct {
	ServiceID     int64     `json:"service_id"`
	StartsAt      time.Time `json:"starts_at"`
	CustomerName  string    `json:"customer_name"`
	CustomerPhone string    `json:"customer_phone"`
	WhatsAppOptIn bool      `json:"whatsapp_opt_in"`
	Notes         string    `json:"notes"`
}

// createOwnerAppointment records a phone booking or walk-in. Owners can
// book outside opening hours and during time off, but never double-book.
func (s *Server) createOwnerAppointment(w http.ResponseWriter, r *http.Request) {
	var in ownerBookingRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	svc, err := s.store.Service(r.Context(), businessID(r), in.ServiceID)
	if err != nil {
		handleErr(w, r, invalid("unknown service"))
		return
	}
	now := s.now()
	req, endsAt, err := booking.ValidateOwner(booking.Request{
		ServiceID: in.ServiceID, StartsAt: in.StartsAt, CustomerName: in.CustomerName,
		CustomerPhone: in.CustomerPhone, WhatsAppOptIn: in.WhatsAppOptIn, Notes: in.Notes,
	}, time.Duration(svc.DurationMin)*time.Minute, s.cfg.DefaultCountryCode, now)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var msgs []booking.PlannedMessage
	if req.StartsAt.After(now) {
		msgs = booking.PlanMessages(req.StartsAt, endsAt, now, req.WhatsAppOptIn)
	}
	id, token, err := s.store.CreateAppointment(r.Context(), businessID(r), req, endsAt, msgs, store.SourceOwner)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"appointment": store.AppointmentView{
			ID: id, ServiceID: svc.ID, ServiceName: svc.Name,
			CustomerName: req.CustomerName, CustomerPhone: req.CustomerPhone,
			StartsAt: req.StartsAt, EndsAt: endsAt, Status: booking.StatusBooked,
			WhatsAppOptIn: req.WhatsAppOptIn, Source: store.SourceOwner, Notes: req.Notes,
		},
		"manage_url": s.cfg.PublicURL + "/c/" + token,
	})
}

type rescheduleRequest struct {
	StartsAt time.Time `json:"starts_at"`
}

func (s *Server) rescheduleAppointment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	var in rescheduleRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	a, err := s.store.Reschedule(r.Context(), businessID(r), id, in.StartsAt, s.now())
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// Time off

func (s *Server) listTimeOff(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.TimeOffFrom(r.Context(), businessID(r), s.now())
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) addTimeOff(w http.ResponseWriter, r *http.Request) {
	var in store.TimeOff
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	switch {
	case in.StartsAt.IsZero() || in.EndsAt.IsZero():
		handleErr(w, r, invalid("start and end are required"))
		return
	case !in.EndsAt.After(in.StartsAt):
		handleErr(w, r, invalid("the end must be after the start"))
		return
	case !in.EndsAt.After(s.now()):
		handleErr(w, r, invalid("that period is already over"))
		return
	case in.EndsAt.Sub(in.StartsAt) > 366*24*time.Hour:
		handleErr(w, r, invalid("time off can be at most a year long"))
		return
	case utf8.RuneCountInString(in.Reason) > 200:
		handleErr(w, r, invalid("reason is too long (max 200 characters)"))
		return
	}
	t, clashes, err := s.store.AddTimeOff(r.Context(), businessID(r), in)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"time_off": t, "clashing_appointments": clashes})
}

func (s *Server) deleteTimeOff(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.store.DeleteTimeOff(r.Context(), businessID(r), id); err != nil {
		handleErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Email verification and change

type tokenRequest struct {
	Token string `json:"token"`
}

func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in tokenRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.store.VerifyEmail(r.Context(), in.Token); err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Thanks, your email is confirmed."})
}

func (s *Server) sendVerification(r *http.Request, id int64) error {
	b, token, err := s.store.CreateEmailVerification(r.Context(), id)
	if err != nil {
		return err
	}
	s.sendEmail(b.OwnerEmail, "Confirm your email for "+s.cfg.CompanyName, fmt.Sprintf(
		"Hi,\n\nPlease confirm this is the login email for %s:\n%s/verify/%s\n\nThe link works for 7 days.\n",
		b.Name, s.cfg.PublicURL, token))
	return nil
}

func (s *Server) resendVerification(w http.ResponseWriter, r *http.Request) {
	if err := s.sendVerification(r, businessID(r)); err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "We've sent a new confirmation link."})
}

type changeEmailRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) changeEmail(w http.ResponseWriter, r *http.Request) {
	var in changeEmailRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.checkPassword(r, in.Password); err != nil {
		handleErr(w, r, err)
		return
	}
	email, err := cleanEmail(in.Email)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	old, err := s.store.BusinessByID(r.Context(), businessID(r))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.store.ChangeEmail(r.Context(), old.ID, email); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.sendVerification(r, old.ID); err != nil {
		handleErr(w, r, err)
		return
	}
	// Tell the old address, in case this wasn't the owner.
	s.sendEmail(old.OwnerEmail, "Your "+s.cfg.CompanyName+" login email was changed", fmt.Sprintf(
		"The login email for %s was changed to %s.\nIf you didn't do this, contact %s straight away.\n",
		old.Name, email, s.supportContact()))
	s.getBusinessByID(w, r, old.ID)
}

func (s *Server) supportContact() string {
	if s.cfg.SupportEmail != "" {
		return s.cfg.SupportEmail
	}
	return "support"
}

// Export

// exportCSV downloads every appointment, for the owner's records or to
// move to another system.
func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.BusinessByID(r.Context(), businessID(r))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	appts, err := s.store.AllAppointments(r.Context(), b.ID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-appointments.csv"`, b.Slug))
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "date", "start", "end", "service", "customer", "phone", "status", "whatsapp", "source", "notes"})
	for _, a := range appts {
		cw.Write([]string{
			strconv.FormatInt(a.ID, 10),
			a.StartsAt.In(loc).Format(time.DateOnly),
			a.StartsAt.In(loc).Format("15:04"),
			a.EndsAt.In(loc).Format("15:04"),
			csvSafe(a.ServiceName), csvSafe(a.CustomerName), csvSafe(a.CustomerPhone),
			string(a.Status), strconv.FormatBool(a.WhatsAppOptIn), a.Source, csvSafe(a.Notes),
		})
	}
	cw.Flush()
}

// csvSafe stops spreadsheet apps treating customer-typed text as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
