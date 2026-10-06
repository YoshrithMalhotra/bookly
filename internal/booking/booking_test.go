package booking

import (
	"errors"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func clocks(ts []time.Time, loc *time.Location) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.In(loc).Format("15:04")
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAvailableSlots(t *testing.T) {
	london := mustLoc(t, "Europe/London")
	weekdays := func(open, close int) []OpeningHours {
		var hs []OpeningHours
		for d := time.Monday; d <= time.Friday; d++ {
			hs = append(hs, OpeningHours{d, open, close})
		}
		return hs
	}
	mon := Date{2026, time.November, 2} // a Monday
	at := func(d Date, h, m int) time.Time { return d.At(h*60+m, london) }
	longAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		date     Date
		hours    []OpeningHours
		duration time.Duration
		busy     []Interval
		now      time.Time
		want     []string
	}{
		{
			name:  "closed day",
			date:  Date{2026, time.November, 1}, // Sunday
			hours: weekdays(9*60, 12*60), duration: time.Hour, now: longAgo,
			want: []string{},
		},
		{
			name:  "open day",
			date:  mon,
			hours: weekdays(9*60, 12*60), duration: time.Hour, now: longAgo,
			want: []string{"09:00", "10:00", "11:00"},
		},
		{
			name:  "slot that does not fit before closing is dropped",
			date:  mon,
			hours: weekdays(9*60, 11*60+30), duration: time.Hour, now: longAgo,
			want: []string{"09:00", "10:00"},
		},
		{
			name:  "booking in the middle",
			date:  mon,
			hours: weekdays(9*60, 12*60), duration: time.Hour, now: longAgo,
			busy: []Interval{{at(mon, 10, 15), at(mon, 10, 45)}},
			want: []string{"09:00", "11:00"},
		},
		{
			name:  "fully booked",
			date:  mon,
			hours: weekdays(9*60, 12*60), duration: time.Hour, now: longAgo,
			busy: []Interval{{at(mon, 9, 0), at(mon, 12, 0)}},
			want: []string{},
		},
		{
			name:  "back-to-back booking does not block",
			date:  mon,
			hours: weekdays(9*60, 12*60), duration: time.Hour, now: longAgo,
			busy: []Interval{{at(mon, 8, 0), at(mon, 9, 0)}, {at(mon, 12, 0), at(mon, 13, 0)}},
			want: []string{"09:00", "10:00", "11:00"},
		},
		{
			name:  "past slots removed",
			date:  mon,
			hours: weekdays(9*60, 12*60), duration: time.Hour, now: at(mon, 10, 0),
			want: []string{"11:00"},
		},
		{
			name:     "DST end, Europe/London last Sunday of October",
			date:     Date{2026, time.October, 25},
			hours:    []OpeningHours{{time.Sunday, 0, 4 * 60}},
			duration: time.Hour, now: longAgo,
			// 01:00 happens twice; the day from 00:00 to 04:00 lasts 5 hours.
			want: []string{"00:00", "01:00", "01:00", "02:00", "03:00"},
		},
		{
			name:     "DST date during normal hours is unaffected",
			date:     Date{2026, time.October, 25},
			hours:    []OpeningHours{{time.Sunday, 9 * 60, 11 * 60}},
			duration: 30 * time.Minute, now: longAgo,
			want: []string{"09:00", "09:30", "10:00", "10:30"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clocks(AvailableSlots(tt.date, london, tt.hours, tt.duration, tt.busy, tt.now), london)
			if !equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	london := mustLoc(t, "Europe/London")
	hours := []OpeningHours{{time.Monday, 9 * 60, 17 * 60}}
	mon := Date{2026, time.November, 2}
	now := mon.At(8*60, london)
	ok := Request{ServiceID: 1, StartsAt: mon.At(10*60, london), CustomerName: "  Ada   Lovelace ", CustomerPhone: "07700 900123"}

	got, end, err := Validate(ok, london, hours, 30*time.Minute, "44", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomerName != "Ada Lovelace" || got.CustomerPhone != "+447700900123" {
		t.Errorf("not cleaned: %+v", got)
	}
	if !end.Equal(ok.StartsAt.Add(30 * time.Minute)) {
		t.Errorf("end = %v", end)
	}

	bad := []struct {
		name string
		mod  func(r *Request)
	}{
		{"empty name", func(r *Request) { r.CustomerName = "  " }},
		{"bad phone", func(r *Request) { r.CustomerPhone = "abc" }},
		{"past", func(r *Request) { r.StartsAt = mon.At(7*60, london) }},
		{"off grid", func(r *Request) { r.StartsAt = mon.At(10*60+10, london) }},
		{"closed day", func(r *Request) { r.StartsAt = mon.At(10*60, london).AddDate(0, 0, 1) }},
		{"too far ahead", func(r *Request) { r.StartsAt = r.StartsAt.AddDate(1, 0, 0) }},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			r := ok
			tt.mod(&r)
			_, _, err := Validate(r, london, hours, 30*time.Minute, "44", now)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("want ValidationError, got %v", err)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		in, cc, want string
		wantErr      bool
	}{
		{"+44 7700 900123", "", "+447700900123", false},
		{"0044 7700 900123", "", "+447700900123", false},
		{"07700 900123", "44", "+447700900123", false},
		{"(98765) 43210", "91", "+919876543210", false},
		{"07700 900123", "", "", true},
		{"+12", "", "", true},
		{"+44 7700 9001x3", "", "", true},
		{"+1234567890123456", "", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizePhone(tt.in, tt.cc)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("NormalizePhone(%q, %q) = %q, %v", tt.in, tt.cc, got, err)
		}
	}
}

func TestPlanMessages(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		startsIn time.Duration
		optIn    bool
		want     []PlannedMessage
	}{
		{"no opt-in, nothing", 48 * time.Hour, false, nil},
		{"two days ahead", 48 * time.Hour, true, []PlannedMessage{
			{MessageReminder, now.Add(24 * time.Hour)},
			{MessageReview, now.Add(49*time.Hour + ReviewDelay)},
		}},
		{"tomorrow morning: remind now", 5 * time.Hour, true, []PlannedMessage{
			{MessageReminder, now},
			{MessageReview, now.Add(6*time.Hour + ReviewDelay)},
		}},
		{"within the hour: no reminder", time.Hour, true, []PlannedMessage{
			{MessageReview, now.Add(2*time.Hour + ReviewDelay)},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := now.Add(tt.startsIn)
			got := PlanMessages(start, start.Add(time.Hour), now, tt.optIn)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i].Type != tt.want[i].Type || !got[i].SendAt.Equal(tt.want[i].SendAt) {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	tests := []struct {
		name string
		m    DueMessage
		want Action
	}{
		{"reminder for booked", DueMessage{Type: MessageReminder, Status: StatusBooked, OptIn: true, StartsAt: future}, ActionSend},
		{"reminder for confirmed", DueMessage{Type: MessageReminder, Status: StatusConfirmed, OptIn: true, StartsAt: future}, ActionSend},
		{"reminder after start", DueMessage{Type: MessageReminder, Status: StatusBooked, OptIn: true, StartsAt: past}, ActionCancel},
		{"reminder for cancelled", DueMessage{Type: MessageReminder, Status: StatusCancelled, OptIn: true, StartsAt: future}, ActionCancel},
		{"opted out", DueMessage{Type: MessageReminder, Status: StatusBooked, OptIn: false, StartsAt: future}, ActionCancel},
		{"review when done", DueMessage{Type: MessageReview, Status: StatusDone, OptIn: true, ReviewURL: "https://g.page/r/x"}, ActionSend},
		{"review without url", DueMessage{Type: MessageReview, Status: StatusDone, OptIn: true}, ActionCancel},
		{"review for no-show", DueMessage{Type: MessageReview, Status: StatusNoShow, OptIn: true, ReviewURL: "u"}, ActionCancel},
		{"review not marked yet", DueMessage{Type: MessageReview, Status: StatusBooked, OptIn: true, ReviewURL: "u", EndsAt: past}, ActionDefer},
		{"review never marked", DueMessage{Type: MessageReview, Status: StatusBooked, OptIn: true, ReviewURL: "u", EndsAt: now.Add(-ReviewDeadline - time.Minute)}, ActionCancel},
	}
	for _, tt := range tests {
		if got := Decide(tt.m, now); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 25 * time.Minute, 125 * time.Minute}
	for i, w := range want {
		if got := RetryDelay(i + 1); got != w {
			t.Errorf("RetryDelay(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestCanTransition(t *testing.T) {
	if !CanTransition(StatusBooked, StatusDone) || !CanTransition(StatusDone, StatusNoShow) {
		t.Error("expected allowed")
	}
	if CanTransition(StatusCancelled, StatusBooked) || CanTransition(StatusDone, StatusCancelled) || CanTransition(StatusBooked, StatusBooked) {
		t.Error("expected forbidden")
	}
}
