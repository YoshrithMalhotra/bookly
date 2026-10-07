package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/store"
)

// errNotAccepting means the business's trial ended without a subscription.
var errNotAccepting = errors.New("this business isn't taking online bookings right now")

type hoursJSON struct {
	Weekday int    `json:"weekday"` // 0 = Sunday
	Opens   string `json:"opens"`   // "09:00"
	Closes  string `json:"closes"`
}

func toHoursJSON(hs []booking.OpeningHours) []hoursJSON {
	out := make([]hoursJSON, len(hs))
	for i, h := range hs {
		out[i] = hoursJSON{int(h.Weekday), booking.FormatClock(h.Opens), booking.FormatClock(h.Closes)}
	}
	return out
}

func (s *Server) getPublicBusiness(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.BusinessBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	services, err := s.store.Services(r.Context(), b.ID, true)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	hours, err := s.store.OpeningHours(r.Context(), b.ID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":               b.Name,
		"slug":               b.Slug,
		"timezone":           b.Timezone,
		"services":           services,
		"hours":              toHoursJSON(hours),
		"max_days_ahead":     booking.MaxDaysAhead,
		"accepting_bookings": s.accepting(b),
		"currency":           b.Currency,
	})
}

// bookingContext loads what slot and booking rules need for one service.
func (s *Server) bookingContext(r *http.Request, slug string, serviceID int64) (store.Business, store.Service, *time.Location, []booking.OpeningHours, error) {
	b, err := s.store.BusinessBySlug(r.Context(), slug)
	if err != nil {
		return b, store.Service{}, nil, nil, err
	}
	if !s.accepting(b) {
		return b, store.Service{}, nil, nil, errNotAccepting
	}
	svc, err := s.store.Service(r.Context(), b.ID, serviceID)
	if err != nil {
		return b, svc, nil, nil, &booking.ValidationError{Msg: "unknown service"}
	}
	if !svc.Active {
		return b, svc, nil, nil, &booking.ValidationError{Msg: "this service is no longer offered"}
	}
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return b, svc, nil, nil, err
	}
	hours, err := s.store.OpeningHours(r.Context(), b.ID)
	return b, svc, loc, hours, err
}

func (s *Server) getSlots(w http.ResponseWriter, r *http.Request) {
	date, err := booking.ParseDate(r.URL.Query().Get("date"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	serviceID, err := parseID(r.URL.Query().Get("service_id"))
	if err != nil {
		handleErr(w, r, &booking.ValidationError{Msg: "service_id is required"})
		return
	}
	b, svc, loc, hours, err := s.bookingContext(r, r.PathValue("slug"), serviceID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	now := s.now()
	dayStart := date.At(0, loc)
	dayEnd := booking.DateOf(dayStart.Add(36*time.Hour), loc).At(0, loc)
	slots := []time.Time{}
	if dayStart.Before(now.AddDate(0, 0, booking.MaxDaysAhead)) {
		busy, err := s.store.BusyIntervals(r.Context(), b.ID, dayStart, dayEnd)
		if err != nil {
			handleErr(w, r, err)
			return
		}
		duration := time.Duration(svc.DurationMin) * time.Minute
		slots = booking.AvailableSlots(date, loc, hours, duration, busy, now)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":     date.String(),
		"timezone": b.Timezone,
		"slots":    slots,
	})
}

type bookingRequest struct {
	ServiceID     int64     `json:"service_id"`
	StartsAt      time.Time `json:"starts_at"`
	CustomerName  string    `json:"customer_name"`
	CustomerPhone string    `json:"customer_phone"`
	WhatsAppOptIn bool      `json:"whatsapp_opt_in"`
	Notes         string    `json:"notes"`
}

func (s *Server) createAppointment(w http.ResponseWriter, r *http.Request) {
	var in bookingRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	b, svc, loc, hours, err := s.bookingContext(r, r.PathValue("slug"), in.ServiceID)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	now := s.now()
	req, endsAt, err := booking.Validate(booking.Request{
		ServiceID:     in.ServiceID,
		StartsAt:      in.StartsAt,
		CustomerName:  in.CustomerName,
		CustomerPhone: in.CustomerPhone,
		WhatsAppOptIn: in.WhatsAppOptIn,
		Notes:         in.Notes,
	}, loc, hours, time.Duration(svc.DurationMin)*time.Minute, s.cfg.DefaultCountryCode, now)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	msgs := booking.PlanMessages(req.StartsAt, endsAt, now, req.WhatsAppOptIn)
	id, token, err := s.store.CreateAppointment(r.Context(), b.ID, req, endsAt, msgs, store.SourceOnline)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	s.sendEmail(b.OwnerEmail, "New booking: "+req.CustomerName+", "+req.StartsAt.In(loc).Format("Mon 2 Jan 15:04"),
		fmt.Sprintf("%s booked %s on %s.\nPhone: %s\n%s\nSee your day: %s/owner\n",
			req.CustomerName, svc.Name, req.StartsAt.In(loc).Format("Monday 2 January at 15:04"),
			req.CustomerPhone, noteLine(req.Notes), s.cfg.PublicURL))
	writeJSON(w, http.StatusCreated, map[string]any{
		"appointment": store.AppointmentView{
			ID: id, ServiceID: svc.ID, ServiceName: svc.Name,
			CustomerName: req.CustomerName, CustomerPhone: req.CustomerPhone,
			StartsAt: req.StartsAt, EndsAt: endsAt, Status: booking.StatusBooked,
			WhatsAppOptIn: req.WhatsAppOptIn, Source: store.SourceOnline, Notes: req.Notes,
		},
		"business":     map[string]string{"name": b.Name, "slug": b.Slug, "timezone": b.Timezone},
		"manage_token": token,
		"manage_url":   s.cfg.PublicURL + "/c/" + token,
	})
}

func (s *Server) getManagedAppointment(w http.ResponseWriter, r *http.Request) {
	a, b, err := s.store.AppointmentByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	a.Notes = "" // may be the owner's private note
	canCancel := (a.Status == booking.StatusBooked || a.Status == booking.StatusConfirmed) && a.StartsAt.After(s.now())
	writeJSON(w, http.StatusOK, map[string]any{
		"appointment": a,
		"business":    map[string]string{"name": b.Name, "slug": b.Slug, "timezone": b.Timezone},
		"can_cancel":  canCancel,
	})
}

func (s *Server) cancelManagedAppointment(w http.ResponseWriter, r *http.Request) {
	a, b, err := s.store.AppointmentByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.store.CancelByToken(r.Context(), r.PathValue("token"), s.now()); err != nil {
		handleErr(w, r, err)
		return
	}
	if loc, err := time.LoadLocation(b.Timezone); err == nil {
		when := a.StartsAt.In(loc).Format("Monday 2 January at 15:04")
		s.sendEmail(b.OwnerEmail, "Cancelled: "+a.CustomerName+", "+a.StartsAt.In(loc).Format("Mon 2 Jan 15:04"),
			fmt.Sprintf("%s cancelled their %s on %s. The slot is free again.\n", a.CustomerName, a.ServiceName, when))
	}
	s.getManagedAppointment(w, r)
}

func noteLine(n string) string {
	if n == "" {
		return ""
	}
	return "Note: " + n + "\n"
}
