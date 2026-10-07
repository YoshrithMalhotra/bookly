// Package billing talks to Stripe and decides whether a business has paid.
package billing

import "time"

// StatusTrial is our own free trial, before the owner has subscribed.
// Every other status is copied from the Stripe subscription.
const StatusTrial = "trial"

// Active reports whether a business may take bookings. past_due stays
// active while Stripe retries the card (its dunning emails the owner).
func Active(status string, trialEndsAt, now time.Time) bool {
	switch status {
	case StatusTrial:
		return now.Before(trialEndsAt)
	case "active", "trialing", "past_due":
		return true
	}
	return false
}
