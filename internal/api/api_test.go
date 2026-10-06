package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/YoshrithMalhotra/bookly/internal/config"
	"github.com/YoshrithMalhotra/bookly/internal/testdb"
)

func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t, base, &http.Client{Jar: jar}}
}

// do sends a JSON request and decodes the JSON response into out (if not nil).
func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, data)
		}
	}
	return resp.StatusCode
}

func (c *client) must(want int, method, path string, body any, out any) {
	c.t.Helper()
	var raw json.RawMessage
	got := c.do(method, path, body, &raw)
	if got != want {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, got, want, raw)
	}
	if out != nil {
		json.Unmarshal(raw, out)
	}
}

func setup(t *testing.T) (*httptest.Server, *Server) {
	s := testdb.New(t)
	srv := New(s, config.Config{Env: "dev", PublicURL: "http://bookly.test", DefaultCountryCode: "44"})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

func signup(t *testing.T, base, slug string) *client {
	c := newClient(t, base)
	c.must(201, "POST", "/api/signup", map[string]string{
		"business_name": "Salon " + slug, "slug": slug, "timezone": "Europe/London",
		"email": slug + "@example.com", "password": "correct horse battery",
	}, nil)
	return c
}

func TestBookingFlow(t *testing.T) {
	ts, srv := setup(t)
	// Monday 2 Nov 2026, 08:00 London.
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }

	owner := signup(t, ts.URL, "anna")
	var svc struct{ ID int64 }
	owner.must(201, "POST", "/api/owner/services", map[string]any{"name": "Haircut", "duration_min": 60, "price": "25"}, &svc)
	owner.must(200, "PUT", "/api/owner/hours", []map[string]any{{"weekday": 1, "opens": "09:00", "closes": "12:00"}}, nil)
	owner.must(200, "PUT", "/api/owner/business", map[string]string{
		"name": "Anna's", "timezone": "Europe/London", "google_review_url": "https://g.page/r/anna/review"}, nil)

	customer := newClient(t, ts.URL)
	var biz struct {
		Name     string
		Services []struct{ ID int64 }
	}
	customer.must(200, "GET", "/api/businesses/anna", nil, &biz)
	if biz.Name != "Anna's" || len(biz.Services) != 1 {
		t.Fatalf("business: %+v", biz)
	}

	slotsPath := fmt.Sprintf("/api/businesses/anna/slots?date=2026-11-02&service_id=%d", svc.ID)
	var slots struct{ Slots []time.Time }
	customer.must(200, "GET", slotsPath, nil, &slots)
	if len(slots.Slots) != 3 {
		t.Fatalf("slots: %v", slots.Slots)
	}

	book := map[string]any{"service_id": svc.ID, "starts_at": slots.Slots[1], "customer_name": "Bob",
		"customer_phone": "07700 900123", "whatsapp_opt_in": true}
	var booked struct {
		Appointment struct {
			ID            int64
			CustomerPhone string `json:"customer_phone"`
		}
		ManageToken string `json:"manage_token"`
	}
	customer.must(201, "POST", "/api/businesses/anna/appointments", book, &booked)
	if booked.Appointment.CustomerPhone != "+447700900123" || booked.ManageToken == "" {
		t.Fatalf("booked: %+v", booked)
	}

	customer.must(200, "GET", slotsPath, nil, &slots)
	if len(slots.Slots) != 2 {
		t.Errorf("slot should be gone: %v", slots.Slots)
	}
	customer.must(409, "POST", "/api/businesses/anna/appointments", book, nil)

	// Bad input is a 400 with the standard error shape.
	var e struct{ Error string }
	bad := map[string]any{"service_id": svc.ID, "starts_at": slots.Slots[0], "customer_name": "", "customer_phone": "1"}
	customer.must(400, "POST", "/api/businesses/anna/appointments", bad, &e)
	if e.Error == "" {
		t.Error("missing error message")
	}
	customer.must(404, "GET", "/api/businesses/nope", nil, nil)

	// Owner sees it, with a reminder and review queued.
	var day struct{ Appointments []struct{ ID int64 } }
	owner.must(200, "GET", "/api/owner/appointments?date=2026-11-02", nil, &day)
	if len(day.Appointments) != 1 {
		t.Fatalf("owner appointments: %+v", day)
	}
	var msgs []struct{ Type, Status string }
	owner.must(200, "GET", fmt.Sprintf("/api/owner/appointments/%d/messages", booked.Appointment.ID), nil, &msgs)
	if len(msgs) != 2 {
		t.Errorf("messages: %+v", msgs)
	}

	// Can't mark done before it happens.
	statusPath := fmt.Sprintf("/api/owner/appointments/%d/status", booked.Appointment.ID)
	owner.must(400, "POST", statusPath, map[string]string{"status": "done"}, nil)

	// Customer cancels with their token; slot frees up; messages cancelled.
	var managed struct {
		CanCancel bool `json:"can_cancel"`
	}
	customer.must(200, "GET", "/api/manage/"+booked.ManageToken, nil, &managed)
	if !managed.CanCancel {
		t.Error("should be cancellable")
	}
	customer.must(200, "POST", "/api/manage/"+booked.ManageToken+"/cancel", nil, nil)
	customer.must(400, "POST", "/api/manage/"+booked.ManageToken+"/cancel", nil, nil)
	customer.must(404, "GET", "/api/manage/not-a-token", nil, nil)
	customer.must(200, "GET", slotsPath, nil, &slots)
	if len(slots.Slots) != 3 {
		t.Errorf("slot should be free again: %v", slots.Slots)
	}
	owner.must(200, "GET", fmt.Sprintf("/api/owner/appointments/%d/messages", booked.Appointment.ID), nil, &msgs)
	for _, m := range msgs {
		if m.Status != "cancelled" {
			t.Errorf("message not cancelled: %+v", m)
		}
	}

	// Book again, time passes, owner marks done.
	customer.must(201, "POST", "/api/businesses/anna/appointments", book, &booked)
	now = now.Add(4 * time.Hour)
	owner.must(200, "POST", fmt.Sprintf("/api/owner/appointments/%d/status", booked.Appointment.ID), map[string]string{"status": "done"}, nil)
	var stats struct{ Stats struct{ Done int } }
	owner.must(200, "GET", "/api/owner/stats", nil, &stats)
	if stats.Stats.Done != 1 {
		t.Errorf("stats: %+v", stats)
	}
}

func TestOwnersAreIsolated(t *testing.T) {
	ts, srv := setup(t)
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }

	a := signup(t, ts.URL, "owner-a")
	b := signup(t, ts.URL, "owner-b")
	var svc struct{ ID int64 }
	a.must(201, "POST", "/api/owner/services", map[string]any{"name": "Cut", "duration_min": 30, "price": "10"}, &svc)

	var booked struct{ Appointment struct{ ID int64 } }
	newClient(t, ts.URL).must(201, "POST", "/api/businesses/owner-a/appointments", map[string]any{
		"service_id": svc.ID, "starts_at": "2026-11-02T10:00:00Z", "customer_name": "C", "customer_phone": "+447700900123",
	}, &booked)

	var day struct{ Appointments []any }
	b.must(200, "GET", "/api/owner/appointments?date=2026-11-02", nil, &day)
	if len(day.Appointments) != 0 {
		t.Error("owner B can see owner A's appointments")
	}
	b.must(404, "POST", fmt.Sprintf("/api/owner/appointments/%d/status", booked.Appointment.ID), map[string]string{"status": "cancelled"}, nil)
	b.must(404, "PUT", fmt.Sprintf("/api/owner/services/%d", svc.ID), map[string]any{"name": "X", "duration_min": 30, "price": "1", "active": true}, nil)
	var msgs []any
	b.must(200, "GET", fmt.Sprintf("/api/owner/appointments/%d/messages", booked.Appointment.ID), nil, &msgs)
	if len(msgs) != 0 {
		t.Error("owner B can see owner A's messages")
	}
	// Owner B can't book A's service through B's page.
	newClient(t, ts.URL).must(400, "POST", "/api/businesses/owner-b/appointments", map[string]any{
		"service_id": svc.ID, "starts_at": "2026-11-02T11:00:00Z", "customer_name": "C", "customer_phone": "+447700900123",
	}, nil)
}

func TestAuth(t *testing.T) {
	ts, _ := setup(t)
	signup(t, ts.URL, "salon")

	anon := newClient(t, ts.URL)
	anon.must(401, "GET", "/api/owner/business", nil, nil)
	anon.must(409, "POST", "/api/signup", map[string]string{"business_name": "X", "slug": "salon",
		"timezone": "Europe/London", "email": "other@example.com", "password": "long enough pw"}, nil)
	anon.must(400, "POST", "/api/signup", map[string]string{"business_name": "X", "slug": "api",
		"timezone": "Europe/London", "email": "x@example.com", "password": "long enough pw"}, nil)
	anon.must(400, "POST", "/api/signup", map[string]string{"business_name": "X", "slug": "ok-slug",
		"timezone": "Mars/Base", "email": "x@example.com", "password": "long enough pw"}, nil)
	anon.must(401, "POST", "/api/login", map[string]string{"email": "salon@example.com", "password": "wrong password"}, nil)
	anon.must(401, "POST", "/api/login", map[string]string{"email": "nobody@example.com", "password": "wrong password"}, nil)

	anon.must(200, "POST", "/api/login", map[string]string{"email": "SALON@example.com", "password": "correct horse battery"}, nil)
	anon.must(200, "GET", "/api/owner/business", nil, nil)
	anon.must(204, "POST", "/api/logout", nil, nil)
	anon.must(401, "GET", "/api/owner/business", nil, nil)
}

func TestCSRFAndRateLimit(t *testing.T) {
	ts, _ := setup(t)

	// A cross-site form post: wrong content type.
	resp, err := http.Post(ts.URL+"/api/login", "application/x-www-form-urlencoded", strings.NewReader("email=a"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form post: got %d", resp.StatusCode)
	}

	// A cross-origin fetch.
	req, _ := http.NewRequest("POST", ts.URL+"/api/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin: got %d", resp.StatusCode)
	}

	c := newClient(t, ts.URL)
	got429 := false
	for range 15 {
		if c.do("POST", "/api/login", map[string]string{"email": "x@example.com", "password": "nope"}, nil) == 429 {
			got429 = true
		}
	}
	if !got429 {
		t.Error("login was never rate limited")
	}
}
