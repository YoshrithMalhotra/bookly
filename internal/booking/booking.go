// Package booking holds the core rules: which slots are free, what a valid
// booking looks like, which messages to schedule and when to send them.
// It has no database code, so everything here is plain table-driven tested.
package booking

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Status string

const (
	StatusBooked    Status = "booked"
	StatusConfirmed Status = "confirmed"
	StatusCancelled Status = "cancelled"
	StatusDone      Status = "done"
	StatusNoShow    Status = "no_show"
)

type Appointment struct {
	ID            int64
	BusinessID    int64
	ServiceID     int64
	CustomerName  string
	CustomerPhone string
	StartsAt      time.Time
	EndsAt        time.Time
	Status        Status
	WhatsAppOptIn bool
}

var (
	ErrSlotTaken = errors.New("that slot was just taken, please pick another")
	ErrNotFound  = errors.New("not found")
)

// ValidationError is a problem with user input; the API returns it as a 400.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// MaxDaysAhead limits how far into the future customers can book.
const MaxDaysAhead = 180

// Date is a calendar day with no time zone, e.g. from ?date=2026-10-25.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, invalid("date must look like 2026-01-31")
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// At returns the instant this date has the given wall-clock time in loc.
func (d Date) At(minutes int, loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, minutes/60, minutes%60, 0, 0, loc)
}

func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC).Weekday()
}

// DateOf returns the calendar date of t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	t = t.In(loc)
	return Date{t.Year(), t.Month(), t.Day()}
}

// OpeningHours for one weekday, as minutes since local midnight.
// Weekday follows Go: 0 = Sunday ... 6 = Saturday.
type OpeningHours struct {
	Weekday time.Weekday
	Opens   int
	Closes  int
}

// ParseClock parses "09:30" into minutes since midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, invalid("time must look like 09:30")
	}
	return t.Hour()*60 + t.Minute(), nil
}

// FormatClock turns minutes since midnight into "09:30".
func FormatClock(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

type Interval struct{ Start, End time.Time }

func (i Interval) overlaps(start, end time.Time) bool {
	return start.Before(i.End) && i.Start.Before(end)
}

// CandidateSlots are the start times on date that fit inside opening hours,
// one every duration, ignoring other bookings and the clock.
func CandidateSlots(date Date, loc *time.Location, hours []OpeningHours, duration time.Duration) []time.Time {
	if duration <= 0 {
		return nil
	}
	var slots []time.Time
	for _, h := range hours {
		if h.Weekday != date.Weekday() {
			continue
		}
		// Absolute times, so a DST jump inside opening hours shortens or
		// lengthens the day instead of producing duplicate or missing slots.
		open, close := date.At(h.Opens, loc), date.At(h.Closes, loc)
		for t := open; !t.Add(duration).After(close); t = t.Add(duration) {
			slots = append(slots, t)
		}
	}
	return slots
}

// AvailableSlots is CandidateSlots minus slots that overlap a busy interval
// or start before now.
func AvailableSlots(date Date, loc *time.Location, hours []OpeningHours, duration time.Duration, busy []Interval, now time.Time) []time.Time {
	free := []time.Time{}
	for _, s := range CandidateSlots(date, loc, hours, duration) {
		if !s.After(now) {
			continue
		}
		end := s.Add(duration)
		taken := false
		for _, b := range busy {
			if b.overlaps(s, end) {
				taken = true
				break
			}
		}
		if !taken {
			free = append(free, s)
		}
	}
	return free
}

// Request is what a customer submits to book.
type Request struct {
	ServiceID     int64
	StartsAt      time.Time
	CustomerName  string
	CustomerPhone string
	WhatsAppOptIn bool
}

// Validate checks and cleans a request against the business's rules.
// It returns the cleaned request and the appointment's end time. The
// database still has the final say on double booking (ErrSlotTaken).
func Validate(req Request, loc *time.Location, hours []OpeningHours, duration time.Duration, countryCode string, now time.Time) (Request, time.Time, error) {
	req.CustomerName = strings.Join(strings.Fields(req.CustomerName), " ")
	if req.CustomerName == "" {
		return req, time.Time{}, invalid("please enter your name")
	}
	if utf8.RuneCountInString(req.CustomerName) > 100 {
		return req, time.Time{}, invalid("name is too long")
	}
	phone, err := NormalizePhone(req.CustomerPhone, countryCode)
	if err != nil {
		return req, time.Time{}, err
	}
	req.CustomerPhone = phone

	if !req.StartsAt.After(now) {
		return req, time.Time{}, invalid("that time is in the past")
	}
	if req.StartsAt.After(now.AddDate(0, 0, MaxDaysAhead)) {
		return req, time.Time{}, invalid("bookings open %d days ahead", MaxDaysAhead)
	}
	ok := false
	for _, s := range CandidateSlots(DateOf(req.StartsAt, loc), loc, hours, duration) {
		if s.Equal(req.StartsAt) {
			ok = true
			break
		}
	}
	if !ok {
		return req, time.Time{}, invalid("that time is not a bookable slot")
	}
	return req, req.StartsAt.Add(duration), nil
}

// NormalizePhone returns the number in E.164 (+447700900123). Numbers
// without a + or 00 prefix get countryCode, dropping one trunk 0.
func NormalizePhone(raw, countryCode string) (string, error) {
	s := strings.TrimSpace(raw)
	var digits strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", invalid("phone number has unexpected characters")
		}
	}
	d := digits.String()
	switch {
	case strings.HasPrefix(s, "+"):
	case strings.HasPrefix(d, "00"):
		d = d[2:]
	case countryCode != "":
		d = countryCode + strings.TrimPrefix(d, "0")
	default:
		return "", invalid("please include your country code, e.g. +44 7700 900123")
	}
	if len(d) < 8 || len(d) > 15 || d[0] == '0' {
		return "", invalid("that doesn't look like a valid phone number")
	}
	return "+" + d, nil
}

// CanTransition says whether an owner may move an appointment from one
// status to another. Cancelled is final; done and no-show can be swapped
// to fix a mis-tap.
func CanTransition(from, to Status) bool {
	switch from {
	case StatusBooked:
		return to == StatusConfirmed || to == StatusCancelled || to == StatusDone || to == StatusNoShow
	case StatusConfirmed:
		return to == StatusCancelled || to == StatusDone || to == StatusNoShow
	case StatusDone:
		return to == StatusNoShow
	case StatusNoShow:
		return to == StatusDone
	}
	return false
}
