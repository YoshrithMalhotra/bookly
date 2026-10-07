package api

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestOwnerBookingsTimeOffAndReschedule(t *testing.T) {
	ts, srv := setup(t)
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC) // Monday 08:00 London
	srv.now = func() time.Time { return now }

	owner := signup(t, ts.URL, "ops")
	var svc struct{ ID int64 }
	owner.must(201, "POST", "/api/owner/services", map[string]any{"name": "Cut", "duration_min": 60, "price": "20"}, &svc)
	owner.must(200, "PUT", "/api/owner/hours", []map[string]any{{"weekday": 1, "opens": "09:00", "closes": "13:00"}}, nil)
	slotsPath := fmt.Sprintf("/api/businesses/ops/slots?date=2026-11-02&service_id=%d", svc.ID)
	slots := func() int {
		var out struct{ Slots []string }
		newClient(t, ts.URL).must(200, "GET", slotsPath, nil, &out)
		return len(out.Slots)
	}
	if n := slots(); n != 4 {
		t.Fatalf("slots = %d", n)
	}

	// Walk-in recorded after the fact, and a phone booking outside opening hours.
	walkIn := map[string]any{"service_id": svc.ID, "starts_at": "2026-11-02T07:00:00Z",
		"customer_name": "Walk In", "customer_phone": "07700 900111", "notes": "paid cash"}
	var created struct {
		Appointment struct {
			ID     int64
			Source string
			Notes  string
		}
	}
	owner.must(201, "POST", "/api/owner/appointments", walkIn, &created)
	if created.Appointment.Source != "owner" || created.Appointment.Notes != "paid cash" {
		t.Errorf("created %+v", created)
	}
	phone := map[string]any{"service_id": svc.ID, "starts_at": "2026-11-02T14:00:00Z",
		"customer_name": "Late Call", "customer_phone": "+447700900222", "whatsapp_opt_in": true}
	owner.must(201, "POST", "/api/owner/appointments", phone, &created)
	lateID := created.Appointment.ID
	owner.must(409, "POST", "/api/owner/appointments", phone, nil)
	owner.must(400, "POST", "/api/owner/appointments", map[string]any{"service_id": 99999, "starts_at": "2026-11-02T15:00:00Z",
		"customer_name": "X", "customer_phone": "+447700900222"}, nil)

	// Time off over 10:00–12:00 hides two slots and blocks online booking.
	var off struct {
		TimeOff struct{ ID int64 } `json:"time_off"`
		Clashes int                `json:"clashing_appointments"`
	}
	owner.must(201, "POST", "/api/owner/time-off", map[string]any{
		"starts_at": "2026-11-02T10:00:00Z", "ends_at": "2026-11-02T12:00:00Z", "reason": "Dentist"}, &off)
	if off.Clashes != 0 {
		t.Errorf("clashes = %d", off.Clashes)
	}
	if n := slots(); n != 2 {
		t.Errorf("slots with time off = %d, want 2", n)
	}
	newClient(t, ts.URL).must(409, "POST", "/api/businesses/ops/appointments", map[string]any{
		"service_id": svc.ID, "starts_at": "2026-11-02T10:00:00Z", "customer_name": "C", "customer_phone": "+447700900333"}, nil)
	// ...but the owner can still squeeze someone in.
	owner.must(201, "POST", "/api/owner/appointments", map[string]any{"service_id": svc.ID, "starts_at": "2026-11-02T10:00:00Z",
		"customer_name": "Friend", "customer_phone": "+447700900444"}, nil)
	owner.must(400, "POST", "/api/owner/time-off", map[string]any{"starts_at": "2026-11-02T12:00:00Z", "ends_at": "2026-11-02T11:00:00Z"}, nil)
	var list []struct{ ID int64 }
	owner.must(200, "GET", "/api/owner/time-off", nil, &list)
	if len(list) != 1 {
		t.Errorf("time off list %+v", list)
	}
	other := signup(t, ts.URL, "ops-other")
	other.must(404, "DELETE", fmt.Sprintf("/api/owner/time-off/%d", off.TimeOff.ID), nil, nil)
	owner.must(204, "DELETE", fmt.Sprintf("/api/owner/time-off/%d", off.TimeOff.ID), nil, nil)

	// Reschedule the 14:00 phone booking to 09:00; messages follow it.
	resPath := fmt.Sprintf("/api/owner/appointments/%d/reschedule", lateID)
	owner.must(409, "POST", resPath, map[string]any{"starts_at": "2026-11-02T10:30:00Z"}, nil) // overlaps Friend
	var moved struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	}
	owner.must(200, "POST", resPath, map[string]any{"starts_at": "2026-11-03T09:00:00Z"}, &moved)
	if !moved.EndsAt.Equal(moved.StartsAt.Add(time.Hour)) {
		t.Errorf("moved %+v", moved)
	}
	var msgs []struct {
		Type, Status string
		SendAt       time.Time `json:"send_at"`
	}
	owner.must(200, "GET", fmt.Sprintf("/api/owner/appointments/%d/messages", lateID), nil, &msgs)
	for _, m := range msgs {
		if m.Status != "pending" {
			t.Errorf("message %+v should be pending", m)
		}
		if m.Type == "reminder" && !m.SendAt.Equal(moved.StartsAt.Add(-24*time.Hour)) {
			t.Errorf("reminder not moved: %v", m.SendAt)
		}
	}
	other.must(404, "POST", resPath, map[string]any{"starts_at": "2026-11-04T09:00:00Z"}, nil)
	owner.must(200, "POST", fmt.Sprintf("/api/owner/appointments/%d/status", lateID), map[string]string{"status": "cancelled"}, nil)
	owner.must(400, "POST", resPath, map[string]any{"starts_at": "2026-11-04T09:00:00Z"}, nil)

	// CSV export, with formula injection neutralised.
	owner.must(201, "POST", "/api/owner/appointments", map[string]any{"service_id": svc.ID, "starts_at": "2026-11-05T09:00:00Z",
		"customer_name": "=HYPERLINK(1)", "customer_phone": "+447700900555"}, nil)
	resp, err := owner.http.Get(ts.URL + "/api/owner/export.csv")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	csv := string(body)
	if resp.StatusCode != 200 || !strings.HasPrefix(csv, "id,date,start") || !strings.Contains(csv, "'=HYPERLINK(1)") ||
		!strings.Contains(csv, "2026-11-02,07:00,08:00,Cut,Walk In") {
		t.Errorf("csv %d:\n%s", resp.StatusCode, csv)
	}
	if strings.Contains(csv, "Late Call") == false {
		t.Error("cancelled appointment missing from export")
	}
	r2, _ := http.Get(ts.URL + "/api/owner/export.csv")
	if r2.StatusCode != 401 {
		t.Errorf("anonymous export: %d", r2.StatusCode)
	}
}

func TestNotesHiddenFromCustomer(t *testing.T) {
	ts, srv := setup(t)
	srv.now = func() time.Time { return time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC) }
	owner := signup(t, ts.URL, "notes")
	var svc struct{ ID int64 }
	owner.must(201, "POST", "/api/owner/services", map[string]any{"name": "Cut", "duration_min": 30, "price": "1"}, &svc)
	var created struct {
		ManageURL string `json:"manage_url"`
	}
	owner.must(201, "POST", "/api/owner/appointments", map[string]any{"service_id": svc.ID, "starts_at": "2026-11-02T10:00:00Z",
		"customer_name": "C", "customer_phone": "+447700900123", "notes": "rude last time"}, &created)
	token := created.ManageURL[strings.LastIndex(created.ManageURL, "/")+1:]
	var managed struct{ Appointment struct{ Notes string } }
	newClient(t, ts.URL).must(200, "GET", "/api/manage/"+token, nil, &managed)
	if managed.Appointment.Notes != "" {
		t.Error("customer can see the owner's note")
	}
}

func TestEmailVerificationAndChange(t *testing.T) {
	ts, srv := setup(t)
	mail := make(chanMailer, 20)
	srv.mailer = mail
	owner := signup(t, ts.URL, "verify")
	e := waitEmail(t, mail, "confirm your email")
	tokenRe := regexp.MustCompile(`/verify/([A-Za-z0-9_-]+)`)
	token := tokenRe.FindStringSubmatch(e.body)[1]

	var b struct {
		EmailVerified bool   `json:"email_verified"`
		OwnerEmail    string `json:"owner_email"`
		Currency      string
	}
	owner.must(200, "GET", "/api/owner/business", nil, &b)
	if b.EmailVerified || b.Currency != "GBP" {
		t.Fatalf("new account: %+v", b)
	}
	anon := newClient(t, ts.URL)
	anon.must(400, "POST", "/api/email/verify", map[string]string{"token": "nope"}, nil)
	anon.must(200, "POST", "/api/email/verify", map[string]string{"token": token}, nil)
	anon.must(400, "POST", "/api/email/verify", map[string]string{"token": token}, nil)
	owner.must(200, "GET", "/api/owner/business", nil, &b)
	if !b.EmailVerified {
		t.Error("not verified")
	}

	signup(t, ts.URL, "taken")
	owner.must(400, "PUT", "/api/owner/email", map[string]string{"email": "new@example.com", "password": "wrong"}, nil)
	owner.must(409, "PUT", "/api/owner/email", map[string]string{"email": "taken@example.com", "password": "correct horse battery"}, nil)
	owner.must(200, "PUT", "/api/owner/email", map[string]string{"email": "New@Example.com", "password": "correct horse battery"}, &b)
	if b.OwnerEmail != "new@example.com" || b.EmailVerified {
		t.Errorf("after change: %+v", b)
	}
	waitEmail(t, mail, "login email was changed")
	// The old link can't verify the new address; a fresh one can.
	owner.must(200, "POST", "/api/owner/email/resend", nil, nil)
	e = waitEmail(t, mail, "Confirm your email")
	if e.to != "new@example.com" {
		t.Errorf("verification sent to %s", e.to)
	}
	anon.must(200, "POST", "/api/email/verify", map[string]string{"token": tokenRe.FindStringSubmatch(e.body)[1]}, nil)
	newClient(t, ts.URL).must(200, "POST", "/api/login", map[string]string{"email": "new@example.com", "password": "correct horse battery"}, nil)

	owner.must(400, "PUT", "/api/owner/business", map[string]string{"name": "V", "timezone": "Europe/London", "currency": "pounds"}, nil)
	owner.must(200, "PUT", "/api/owner/business", map[string]string{"name": "V", "timezone": "Europe/London", "currency": "eur"}, &b)
	if b.Currency != "EUR" {
		t.Errorf("currency %q", b.Currency)
	}
}
