package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/billing"
	"github.com/YoshrithMalhotra/bookly/internal/config"
	"github.com/YoshrithMalhotra/bookly/internal/testdb"
)

type sentEmail struct{ to, subject, body string }

type chanMailer chan sentEmail

func (c chanMailer) Send(ctx context.Context, to, subject, body string) error {
	c <- sentEmail{to, subject, body}
	return nil
}

func waitEmail(t *testing.T, c chanMailer, subject string) sentEmail {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-c:
			if strings.Contains(e.subject, subject) {
				return e
			}
		case <-timeout:
			t.Fatalf("no email with subject %q", subject)
		}
	}
}

func TestPasswordReset(t *testing.T) {
	ts, srv := setup(t)
	mail := make(chanMailer, 10)
	srv.mailer = mail

	signup(t, ts.URL, "resetme")
	waitEmail(t, mail, "Welcome")

	other := newClient(t, ts.URL)
	other.must(200, "POST", "/api/login", map[string]string{"email": "resetme@example.com", "password": "correct horse battery"}, nil)

	anon := newClient(t, ts.URL)
	// Unknown email: same response, no email.
	anon.must(200, "POST", "/api/password/forgot", map[string]string{"email": "nobody@example.com"}, nil)
	anon.must(200, "POST", "/api/password/forgot", map[string]string{"email": "ResetMe@example.com"}, nil)
	e := waitEmail(t, mail, "Reset your")
	if e.to != "resetme@example.com" {
		t.Errorf("sent to %q", e.to)
	}
	token := regexp.MustCompile(`/reset/([A-Za-z0-9_-]+)`).FindStringSubmatch(e.body)[1]

	anon.must(400, "POST", "/api/password/reset", map[string]string{"token": token, "password": "short"}, nil)
	anon.must(400, "POST", "/api/password/reset", map[string]string{"token": "wrong", "password": "a brand new password"}, nil)
	anon.must(200, "POST", "/api/password/reset", map[string]string{"token": token, "password": "a brand new password"}, nil)
	anon.must(200, "GET", "/api/owner/business", nil, nil) // logged in by the reset
	anon.must(400, "POST", "/api/password/reset", map[string]string{"token": token, "password": "another new password"}, nil)

	other.must(401, "GET", "/api/owner/business", nil, nil) // old sessions logged out
	newClient(t, ts.URL).must(401, "POST", "/api/login", map[string]string{"email": "resetme@example.com", "password": "correct horse battery"}, nil)
	newClient(t, ts.URL).must(200, "POST", "/api/login", map[string]string{"email": "resetme@example.com", "password": "a brand new password"}, nil)
}

func TestChangePasswordAndDeleteAccount(t *testing.T) {
	ts, srv := setup(t)
	srv.now = func() time.Time { return time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC) }
	owner := signup(t, ts.URL, "leaving")
	other := newClient(t, ts.URL)
	other.must(200, "POST", "/api/login", map[string]string{"email": "leaving@example.com", "password": "correct horse battery"}, nil)

	owner.must(400, "PUT", "/api/owner/password", map[string]string{"current_password": "nope", "new_password": "a brand new password"}, nil)
	owner.must(200, "PUT", "/api/owner/password", map[string]string{"current_password": "correct horse battery", "new_password": "a brand new password"}, nil)
	owner.must(200, "GET", "/api/owner/business", nil, nil)
	other.must(401, "GET", "/api/owner/business", nil, nil)

	// Give it data so the cascade is exercised.
	var svc struct{ ID int64 }
	owner.must(201, "POST", "/api/owner/services", map[string]any{"name": "Cut", "duration_min": 30, "price": "10"}, &svc)
	newClient(t, ts.URL).must(201, "POST", "/api/businesses/leaving/appointments", map[string]any{
		"service_id": svc.ID, "starts_at": "2026-11-02T10:00:00Z", "customer_name": "C", "customer_phone": "+447700900123", "whatsapp_opt_in": true,
	}, nil)

	owner.must(400, "DELETE", "/api/owner/account", map[string]string{"password": "wrong"}, nil)
	owner.must(204, "DELETE", "/api/owner/account", map[string]string{"password": "a brand new password"}, nil)
	owner.must(401, "GET", "/api/owner/business", nil, nil)
	newClient(t, ts.URL).must(404, "GET", "/api/businesses/leaving", nil, nil)
	// The slug and email are free again.
	signup(t, ts.URL, "leaving")
}

// fakeStripe records calls and serves subscriptions from a map.
type fakeStripe struct {
	mu    sync.Mutex
	subs  map[string]string // id -> status
	calls []string
}

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ParseForm()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/v1/customers":
		w.Write([]byte(`{"id":"cus_1"}`))
	case r.URL.Path == "/v1/checkout/sessions":
		if r.PostForm.Get("customer") != "cus_1" || r.PostForm.Get("line_items[0][price]") != "price_1" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"message":"bad checkout"}}`))
			return
		}
		w.Write([]byte(`{"url":"https://checkout.stripe.test/s1"}`))
	case r.URL.Path == "/v1/billing_portal/sessions":
		w.Write([]byte(`{"url":"https://billing.stripe.test/p1"}`))
	case strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		if r.Method == "DELETE" {
			f.subs[id] = "canceled"
		}
		fmt.Fprintf(w, `{"id":%q,"customer":"cus_1","status":%q,"current_period_end":1800000000,"metadata":{}}`, id, f.subs[id])
	default:
		w.WriteHeader(404)
	}
}

func TestBillingLifecycle(t *testing.T) {
	s := testdb.New(t)
	cfg := config.Config{Env: "dev", PublicURL: "http://bookly.test", DefaultCountryCode: "44", TrialDays: 14,
		StripeSecretKey: "sk_test", StripeWebhookSecret: "whsec_test", StripePriceID: "price_1"}
	srv := New(s, cfg)
	fs := &fakeStripe{subs: map[string]string{}}
	stripeSrv := httptest.NewServer(fs)
	defer stripeSrv.Close()
	srv.stripe.BaseURL = stripeSrv.URL
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	owner := signup(t, ts.URL, "payer")
	var svc struct{ ID int64 }
	owner.must(201, "POST", "/api/owner/services", map[string]any{"name": "Cut", "duration_min": 30, "price": "10"}, &svc)
	book := func(start string) int {
		return newClient(t, ts.URL).do("POST", "/api/businesses/payer/appointments", map[string]any{
			"service_id": svc.ID, "starts_at": start, "customer_name": "C", "customer_phone": "+447700900123"}, nil)
	}

	var bill struct {
		Status string
		Active bool
	}
	owner.must(200, "GET", "/api/owner/billing", nil, &bill)
	if bill.Status != "trial" || !bill.Active {
		t.Fatalf("new account should be on an active trial: %+v", bill)
	}
	if got := book("2026-11-02T10:00:00Z"); got != 201 {
		t.Fatalf("booking during trial: %d", got)
	}

	// Trial over: booking page closes.
	now = now.AddDate(0, 0, 15)
	var pub struct {
		Accepting bool `json:"accepting_bookings"`
	}
	newClient(t, ts.URL).must(200, "GET", "/api/businesses/payer", nil, &pub)
	if pub.Accepting {
		t.Error("should not accept bookings after the trial")
	}
	if got := book("2026-11-23T10:00:00Z"); got != 403 {
		t.Errorf("booking after trial: got %d, want 403", got)
	}
	owner.must(409, "POST", "/api/owner/billing/portal", nil, nil) // no Stripe customer yet
}

func TestBillingLifecycleCheckout(t *testing.T) {
	s := testdb.New(t)
	cfg := config.Config{Env: "dev", PublicURL: "http://bookly.test", DefaultCountryCode: "44", TrialDays: 0,
		StripeSecretKey: "sk_test", StripeWebhookSecret: "whsec_test", StripePriceID: "price_1"}
	srv := New(s, cfg)
	fs := &fakeStripe{subs: map[string]string{}}
	stripeSrv := httptest.NewServer(fs)
	defer stripeSrv.Close()
	srv.stripe.BaseURL = stripeSrv.URL
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	owner := signup(t, ts.URL, "payer")
	var svc struct{ ID int64 }
	owner.must(201, "POST", "/api/owner/services", map[string]any{"name": "Cut", "duration_min": 30, "price": "10"}, &svc)
	book := func(start string) int {
		return newClient(t, ts.URL).do("POST", "/api/businesses/payer/appointments", map[string]any{
			"service_id": svc.ID, "starts_at": start, "customer_name": "C", "customer_phone": "+447700900123"}, nil)
	}
	if got := book("2026-11-02T10:00:00Z"); got != 403 {
		t.Fatalf("no trial, no subscription: got %d", got)
	}

	var out struct{ URL string }
	owner.must(200, "POST", "/api/owner/billing/checkout", nil, &out)
	if out.URL != "https://checkout.stripe.test/s1" {
		t.Fatalf("checkout url %q", out.URL)
	}

	webhook := func(evType string, object string) int {
		payload := []byte(fmt.Sprintf(`{"id":"evt_%d","type":%q,"data":{"object":%s}}`, time.Now().UnixNano(), evType, object))
		req, _ := http.NewRequest("POST", ts.URL+"/api/stripe/webhook", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("Stripe-Signature", billing.SignForTest(payload, "whsec_test", now))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Forged event is rejected.
	req, _ := http.NewRequest("POST", ts.URL+"/api/stripe/webhook", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=1,v1=00")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 400 {
		t.Errorf("forged webhook: %d", resp.StatusCode)
	}

	fs.subs["sub_1"] = "active"
	if got := webhook("checkout.session.completed", `{"id":"cs_1","subscription":"sub_1","customer":"cus_1","client_reference_id":"1"}`); got != 200 {
		t.Fatalf("webhook: %d", got)
	}
	var bill struct {
		Status string
		Active bool
	}
	owner.must(200, "GET", "/api/owner/billing", nil, &bill)
	if bill.Status != "active" || !bill.Active {
		t.Fatalf("after checkout: %+v", bill)
	}
	if got := book("2026-11-02T10:00:00Z"); got != 201 {
		t.Errorf("booking with subscription: %d", got)
	}
	owner.must(409, "POST", "/api/owner/billing/checkout", nil, nil)
	owner.must(200, "POST", "/api/owner/billing/portal", nil, &out)

	// Unrelated events are acknowledged and ignored.
	if got := webhook("invoice.paid", `{"id":"in_1"}`); got != 200 {
		t.Errorf("ignored event: %d", got)
	}

	// Cancelled in the portal.
	fs.subs["sub_1"] = "canceled"
	webhook("customer.subscription.deleted", `{"id":"sub_1"}`)
	owner.must(200, "GET", "/api/owner/billing", nil, &bill)
	if bill.Status != "canceled" || bill.Active {
		t.Errorf("after cancel: %+v", bill)
	}
	if got := book("2026-11-02T11:00:00Z"); got != 403 {
		t.Errorf("booking after cancel: %d", got)
	}

	// Resubscribe with a new subscription; a late event from the old one must not win.
	fs.subs["sub_2"] = "active"
	webhook("customer.subscription.created", `{"id":"sub_2"}`)
	webhook("customer.subscription.updated", `{"id":"sub_1"}`) // still canceled
	owner.must(200, "GET", "/api/owner/billing", nil, &bill)
	if bill.Status != "active" {
		t.Errorf("old subscription overwrote new one: %+v", bill)
	}

	// Deleting the account cancels the live subscription.
	owner.must(204, "DELETE", "/api/owner/account", map[string]string{"password": "correct horse battery"}, nil)
	if fs.subs["sub_2"] != "canceled" {
		t.Error("subscription not cancelled on account deletion")
	}
}
