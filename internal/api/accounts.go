package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/YoshrithMalhotra/bookly/internal/billing"
	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/store"
)

// getConfig is what the frontend needs to know about this deployment.
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"company_name":    s.cfg.CompanyName,
		"support_email":   s.cfg.SupportEmail,
		"billing_enabled": s.cfg.BillingEnabled(),
		"price_label":     s.cfg.PriceLabel,
		"trial_days":      s.cfg.TrialDays,
	})
}

// accepting reports whether a business may take new bookings.
func (s *Server) accepting(b store.Business) bool {
	return !s.cfg.BillingEnabled() || billing.Active(b.SubscriptionStatus, b.TrialEndsAt, s.now())
}

// sendEmail sends in the background so the request doesn't wait on SMTP
// (and so response time doesn't reveal whether an account exists).
func (s *Server) sendEmail(to, subject, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.mailer.Send(ctx, to, subject, body); err != nil {
			slog.Error("send email", "subject", subject, "error", err)
		}
	}()
}

func validPassword(p string) error {
	if len(p) < 10 || len(p) > 72 {
		return invalid("password must be 10 to 72 characters")
	}
	return nil
}

// Password reset

type forgotRequest struct {
	Email string `json:"email"`
}

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in forgotRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	b, token, err := s.store.CreatePasswordReset(r.Context(), email)
	switch {
	case err == nil:
		s.sendEmail(b.OwnerEmail, "Reset your "+s.cfg.CompanyName+" password", fmt.Sprintf(
			"Hi,\n\nSomeone asked to reset the password for %s on %s.\n\n"+
				"Choose a new password here (the link works for 1 hour, once):\n%s/reset/%s\n\n"+
				"If this wasn't you, ignore this email; your password stays the same.\n",
			b.Name, s.cfg.CompanyName, s.cfg.PublicURL, token))
	case errors.Is(err, booking.ErrNotFound):
		// Same answer either way, so this can't be used to find accounts.
	default:
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "If that email has an account, we've sent a reset link."})
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in resetRequest
	if err := decode(w, r, &in); err != nil {
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
	id, err := s.store.ResetPassword(r.Context(), in.Token, hash)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.startSession(w, r.Context(), id); err != nil {
		handleErr(w, r, err)
		return
	}
	s.getBusinessByID(w, r, id)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in changePasswordRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.checkPassword(r, in.CurrentPassword); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := validPassword(in.NewPassword); err != nil {
		handleErr(w, r, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcryptCost)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	c, _ := r.Cookie(sessionCookie)
	if err := s.store.ChangePassword(r.Context(), businessID(r), hash, c.Value); err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Password changed. Other devices have been logged out."})
}

func (s *Server) checkPassword(r *http.Request, password string) error {
	hash, err := s.store.PasswordHash(r.Context(), businessID(r))
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		return invalid("current password is wrong")
	}
	return nil
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// deleteAccount permanently removes the business and all its data, and
// cancels its Stripe subscription so the owner isn't charged again.
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in deleteAccountRequest
	if err := decode(w, r, &in); err != nil {
		handleErr(w, r, err)
		return
	}
	if err := s.checkPassword(r, in.Password); err != nil {
		handleErr(w, r, err)
		return
	}
	b, err := s.store.BusinessByID(r.Context(), businessID(r))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	if s.stripe != nil && b.StripeSubscriptionID != "" && billing.Active(b.SubscriptionStatus, b.TrialEndsAt, s.now()) {
		if err := s.stripe.CancelSubscription(r.Context(), b.StripeSubscriptionID); err != nil {
			slog.Error("cancel subscription on delete", "business_id", b.ID, "error", err)
			writeError(w, http.StatusBadGateway, "couldn't cancel your subscription with Stripe; please try again or contact support")
			return
		}
	}
	if err := s.store.DeleteBusiness(r.Context(), b.ID); err != nil {
		handleErr(w, r, err)
		return
	}
	slog.Info("business deleted", "business_id", b.ID)
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Billing

func (s *Server) getBilling(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.BusinessByID(r.Context(), businessID(r))
	if err != nil {
		handleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":            s.cfg.BillingEnabled(),
		"status":             b.SubscriptionStatus,
		"active":             s.accepting(b),
		"trial_ends_at":      b.TrialEndsAt,
		"current_period_end": b.CurrentPeriodEnd,
		"has_customer":       b.StripeCustomerID != "",
		"price_label":        s.cfg.PriceLabel,
	})
}

func (s *Server) billingBusiness(w http.ResponseWriter, r *http.Request) (store.Business, bool) {
	if s.stripe == nil {
		writeError(w, http.StatusNotFound, "billing is not enabled")
		return store.Business{}, false
	}
	b, err := s.store.BusinessByID(r.Context(), businessID(r))
	if err != nil {
		handleErr(w, r, err)
		return b, false
	}
	return b, true
}

func (s *Server) startCheckout(w http.ResponseWriter, r *http.Request) {
	b, ok := s.billingBusiness(w, r)
	if !ok {
		return
	}
	switch b.SubscriptionStatus {
	case "active", "trialing", "past_due":
		writeError(w, http.StatusConflict, "you already have a subscription; use Manage billing")
		return
	}
	if b.StripeCustomerID == "" {
		id, err := s.stripe.CreateCustomer(r.Context(), b.OwnerEmail, b.Name, b.ID)
		if err != nil {
			s.stripeFailed(w, err)
			return
		}
		if err := s.store.SetStripeCustomer(r.Context(), b.ID, id); err != nil {
			handleErr(w, r, err)
			return
		}
		b.StripeCustomerID = id
	}
	url, err := s.stripe.CheckoutURL(r.Context(), b.StripeCustomerID, s.cfg.StripePriceID, b.ID,
		s.cfg.PublicURL+"/owner/billing?checkout=success", s.cfg.PublicURL+"/owner/billing")
	if err != nil {
		s.stripeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) openPortal(w http.ResponseWriter, r *http.Request) {
	b, ok := s.billingBusiness(w, r)
	if !ok {
		return
	}
	if b.StripeCustomerID == "" {
		writeError(w, http.StatusConflict, "subscribe first")
		return
	}
	url, err := s.stripe.PortalURL(r.Context(), b.StripeCustomerID, s.cfg.PublicURL+"/owner/billing")
	if err != nil {
		s.stripeFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) stripeFailed(w http.ResponseWriter, err error) {
	slog.Error("stripe", "error", err)
	writeError(w, http.StatusBadGateway, "couldn't reach our payment provider, please try again in a minute")
}

// stripeWebhook keeps subscription status in sync. It always re-fetches the
// subscription from Stripe, so events arriving out of order or twice are harmless.
func (s *Server) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.stripe == nil {
		writeError(w, http.StatusNotFound, "billing is not enabled")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "body too large")
		return
	}
	ev, err := billing.VerifyWebhook(payload, r.Header.Get("Stripe-Signature"), s.cfg.StripeWebhookSecret, 5*time.Minute, s.now())
	if err != nil {
		slog.Warn("stripe webhook rejected", "error", err)
		writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	var obj struct {
		ID                string `json:"id"`
		Subscription      string `json:"subscription"`
		ClientReferenceID string `json:"client_reference_id"`
	}
	_ = json.Unmarshal(ev.Data.Object, &obj)

	var subID string
	var fallbackBusiness int64
	switch ev.Type {
	case "checkout.session.completed":
		subID = obj.Subscription
		fallbackBusiness, _ = strconv.ParseInt(obj.ClientReferenceID, 10, 64)
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted",
		"customer.subscription.paused", "customer.subscription.resumed":
		subID = obj.ID
	}
	if subID == "" {
		w.WriteHeader(http.StatusOK) // not an event we care about
		return
	}

	sub, err := s.stripe.GetSubscription(r.Context(), subID)
	if err != nil {
		s.stripeFailed(w, err) // non-2xx makes Stripe retry later
		return
	}
	if sub.BusinessID == 0 {
		sub.BusinessID = fallbackBusiness
	}
	matched, err := s.store.ApplySubscription(r.Context(), sub)
	if err != nil {
		handleErr(w, r, err)
		return
	}
	slog.Info("stripe webhook", "event", ev.Type, "event_id", ev.ID, "subscription", sub.ID,
		"status", sub.Status, "matched", matched)
	w.WriteHeader(http.StatusOK)
}
