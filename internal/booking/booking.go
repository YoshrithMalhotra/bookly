// Package booking holds the core rules: creating and cancelling appointments
// and working out which slots are free.
package booking

import "time"

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

// TODO(week 1): Create — validate input, insert the appointment AND its
// scheduled_messages rows in one transaction, handle double-booking errors.

// TODO(week 1): AvailableSlots — opening hours minus existing appointments,
// computed in the business's timezone.

// TODO(week 2): Cancel — mark cancelled and cancel pending messages.
