package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActive(t *testing.T) {
	now := time.Now()
	tests := []struct {
		status string
		trial  time.Time
		want   bool
	}{
		{StatusTrial, now.Add(time.Hour), true},
		{StatusTrial, now.Add(-time.Hour), false},
		{"active", now.Add(-time.Hour), true},
		{"past_due", now.Add(-time.Hour), true},
		{"canceled", now.Add(time.Hour), false},
		{"unpaid", now, false},
		{"incomplete", now, false},
	}
	for _, tt := range tests {
		if got := Active(tt.status, tt.trial, now); got != tt.want {
			t.Errorf("Active(%q) = %v", tt.status, got)
		}
	}
}

func TestVerifyWebhook(t *testing.T) {
	now := time.Now()
	payload := []byte(`{"id":"evt_1","type":"customer.subscription.updated","data":{"object":{"id":"sub_1"}}}`)
	ev, err := VerifyWebhook(payload, SignForTest(payload, "whsec_x", now), "whsec_x", 5*time.Minute, now)
	if err != nil || ev.Type != "customer.subscription.updated" || string(ev.Data.Object) != `{"id":"sub_1"}` {
		t.Fatalf("got %+v, %v", ev, err)
	}
	if _, err := VerifyWebhook(payload, SignForTest(payload, "other", now), "whsec_x", 5*time.Minute, now); err == nil {
		t.Error("wrong secret accepted")
	}
	if _, err := VerifyWebhook(payload, SignForTest(payload, "whsec_x", now.Add(-time.Hour)), "whsec_x", 5*time.Minute, now); err == nil {
		t.Error("old event accepted")
	}
	if _, err := VerifyWebhook([]byte(`{"id":"evt_2"}`), SignForTest(payload, "whsec_x", now), "whsec_x", 5*time.Minute, now); err == nil {
		t.Error("tampered payload accepted")
	}
	if _, err := VerifyWebhook(payload, "garbage", "whsec_x", 5*time.Minute, now); err == nil {
		t.Error("garbage header accepted")
	}
}

func TestGetSubscriptionReadsPeriodEndFromItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, _ := r.BasicAuth(); u != "sk_test" || r.URL.Path != "/v1/subscriptions/sub_1" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"bad"}}`))
			return
		}
		w.Write([]byte(`{"id":"sub_1","customer":"cus_1","status":"active","metadata":{"business_id":"7"},
			"items":{"data":[{"current_period_end":1800000000}]}}`))
	}))
	defer srv.Close()
	s := &Stripe{SecretKey: "sk_test", BaseURL: srv.URL}
	sub, err := s.GetSubscription(context.Background(), "sub_1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != "active" || sub.CustomerID != "cus_1" || sub.BusinessID != 7 || sub.CurrentPeriodEnd.Unix() != 1800000000 {
		t.Errorf("got %+v", sub)
	}
	if _, err := (&Stripe{SecretKey: "nope", BaseURL: srv.URL}).GetSubscription(context.Background(), "sub_1"); err == nil {
		t.Error("expected error")
	}
}
