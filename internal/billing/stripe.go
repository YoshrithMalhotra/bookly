package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Stripe is a minimal client for the few Stripe APIs Bookly uses.
type Stripe struct {
	SecretKey string
	BaseURL   string // defaults to https://api.stripe.com
	Client    *http.Client
}

type stripeError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (s *Stripe) call(ctx context.Context, method, path string, form url.Values, out any) error {
	base := s.BaseURL
	if base == "" {
		base = "https://api.stripe.com"
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.SecretKey, "")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	if resp.StatusCode >= 300 {
		var se stripeError
		_ = json.Unmarshal(data, &se)
		return fmt.Errorf("stripe: HTTP %d: %s", resp.StatusCode, se.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// CreateCustomer returns the new customer's id.
func (s *Stripe) CreateCustomer(ctx context.Context, email, name string, businessID int64) (string, error) {
	var out struct{ ID string }
	err := s.call(ctx, "POST", "/v1/customers", url.Values{
		"email":                 {email},
		"name":                  {name},
		"metadata[business_id]": {strconv.FormatInt(businessID, 10)},
	}, &out)
	return out.ID, err
}

// CheckoutURL starts a subscription checkout and returns the page to send the owner to.
func (s *Stripe) CheckoutURL(ctx context.Context, customerID, priceID string, businessID int64, successURL, cancelURL string) (string, error) {
	var out struct{ URL string }
	id := strconv.FormatInt(businessID, 10)
	err := s.call(ctx, "POST", "/v1/checkout/sessions", url.Values{
		"mode":                    {"subscription"},
		"customer":                {customerID},
		"client_reference_id":     {id},
		"line_items[0][price]":    {priceID},
		"line_items[0][quantity]": {"1"},
		"subscription_data[metadata][business_id]": {id},
		"allow_promotion_codes":                    {"true"},
		"success_url":                              {successURL},
		"cancel_url":                               {cancelURL},
	}, &out)
	return out.URL, err
}

// PortalURL opens the Stripe customer portal (change card, invoices, cancel).
func (s *Stripe) PortalURL(ctx context.Context, customerID, returnURL string) (string, error) {
	var out struct{ URL string }
	err := s.call(ctx, "POST", "/v1/billing_portal/sessions", url.Values{
		"customer":   {customerID},
		"return_url": {returnURL},
	}, &out)
	return out.URL, err
}

// Subscription is the part of a Stripe subscription Bookly stores.
type Subscription struct {
	ID               string
	CustomerID       string
	Status           string
	CurrentPeriodEnd time.Time
	BusinessID       int64 // from metadata, 0 if missing
}

func (s *Stripe) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	var raw struct {
		ID               string `json:"id"`
		Customer         string `json:"customer"`
		Status           string `json:"status"`
		CurrentPeriodEnd int64  `json:"current_period_end"`
		Metadata         map[string]string
		Items            struct {
			Data []struct {
				CurrentPeriodEnd int64 `json:"current_period_end"`
			}
		}
	}
	if err := s.call(ctx, "GET", "/v1/subscriptions/"+url.PathEscape(id), nil, &raw); err != nil {
		return Subscription{}, err
	}
	end := raw.CurrentPeriodEnd
	if end == 0 && len(raw.Items.Data) > 0 { // newer API versions moved it to items
		end = raw.Items.Data[0].CurrentPeriodEnd
	}
	sub := Subscription{ID: raw.ID, CustomerID: raw.Customer, Status: raw.Status}
	if end > 0 {
		sub.CurrentPeriodEnd = time.Unix(end, 0).UTC()
	}
	sub.BusinessID, _ = strconv.ParseInt(raw.Metadata["business_id"], 10, 64)
	return sub, nil
}

// CancelSubscription cancels immediately (used when an owner deletes their account).
func (s *Stripe) CancelSubscription(ctx context.Context, id string) error {
	return s.call(ctx, "DELETE", "/v1/subscriptions/"+url.PathEscape(id), nil, nil)
}

// Event is a verified webhook event. Object holds data.object.
type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

var ErrBadSignature = errors.New("invalid Stripe signature")

// VerifyWebhook checks the Stripe-Signature header (HMAC-SHA256 over
// "timestamp.payload") and rejects events older than tolerance.
func VerifyWebhook(payload []byte, header, secret string, tolerance time.Duration, now time.Time) (Event, error) {
	var ts int64
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return Event{}, ErrBadSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return Event{}, ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	mac.Write(payload)
	want := mac.Sum(nil)
	for _, sig := range sigs {
		got, err := hex.DecodeString(sig)
		if err == nil && hmac.Equal(got, want) {
			var ev Event
			if err := json.Unmarshal(payload, &ev); err != nil {
				return Event{}, fmt.Errorf("decode event: %w", err)
			}
			return ev, nil
		}
	}
	return Event{}, ErrBadSignature
}

// SignForTest builds a valid Stripe-Signature header; used by tests.
func SignForTest(payload []byte, secret string, ts time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts.Unix())
	mac.Write(payload)
	return fmt.Sprintf("t=%d,v1=%s", ts.Unix(), hex.EncodeToString(mac.Sum(nil)))
}
