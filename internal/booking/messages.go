package booking

import (
	"fmt"
	"time"
)

type MessageType string

const (
	MessageReminder MessageType = "reminder"
	MessageReview   MessageType = "review"
)

const (
	ReminderLead   = 24 * time.Hour // reminder goes out this long before the start
	MinReminderGap = 2 * time.Hour  // no reminder for bookings made closer than this
	ReviewDelay    = 2 * time.Hour  // review request goes out this long after the end
	ReviewDeadline = 72 * time.Hour // stop waiting for the owner to mark it done
	ReviewRecheck  = time.Hour      // how often to re-check an unmarked appointment
	MaxAttempts    = 5
)

type PlannedMessage struct {
	Type   MessageType
	SendAt time.Time
}

// PlanMessages lists the messages to schedule for a new appointment.
// Nothing is sent to customers who did not opt in.
func PlanMessages(startsAt, endsAt, now time.Time, optIn bool) []PlannedMessage {
	if !optIn {
		return nil
	}
	var msgs []PlannedMessage
	if startsAt.Sub(now) >= MinReminderGap {
		sendAt := startsAt.Add(-ReminderLead)
		if sendAt.Before(now) {
			sendAt = now
		}
		msgs = append(msgs, PlannedMessage{MessageReminder, sendAt})
	}
	msgs = append(msgs, PlannedMessage{MessageReview, endsAt.Add(ReviewDelay)})
	return msgs
}

type Action int

const (
	ActionSend Action = iota
	ActionCancel
	ActionDefer
)

// DueMessage is a pending message whose send_at has passed, with the
// appointment state the worker needs to decide what to do with it.
type DueMessage struct {
	Type      MessageType
	Status    Status
	OptIn     bool
	StartsAt  time.Time
	EndsAt    time.Time
	ReviewURL string
}

// Decide what the worker does with a due message.
func Decide(m DueMessage, now time.Time) Action {
	if !m.OptIn || m.Status == StatusCancelled {
		return ActionCancel
	}
	switch m.Type {
	case MessageReminder:
		if (m.Status == StatusBooked || m.Status == StatusConfirmed) && m.StartsAt.After(now) {
			return ActionSend
		}
		return ActionCancel
	case MessageReview:
		switch m.Status {
		case StatusDone:
			if m.ReviewURL == "" {
				return ActionCancel
			}
			return ActionSend
		case StatusBooked, StatusConfirmed:
			// The owner hasn't marked the visit yet; wait a while.
			if now.After(m.EndsAt.Add(ReviewDeadline)) {
				return ActionCancel
			}
			return ActionDefer
		}
	}
	return ActionCancel
}

// RetryDelay is the backoff after a failed send: 1m, 5m, 25m, 2h5m.
func RetryDelay(attempts int) time.Duration {
	d := time.Minute
	for i := 1; i < attempts; i++ {
		d *= 5
	}
	return d
}

// MessageContent is everything needed to render one message.
type MessageContent struct {
	CustomerName string
	BusinessName string
	ServiceName  string
	StartsAt     time.Time
	Location     *time.Location
	ManageURL    string
	ReviewURL    string
}

// Vars are the template variables, numbered the way WhatsApp templates
// expect: {{1}}, {{2}}, ... Approved templates must use the same order.
func (c MessageContent) Vars(t MessageType) []string {
	if t == MessageReview {
		return []string{c.CustomerName, c.BusinessName, c.ReviewURL}
	}
	when := c.StartsAt.In(c.Location).Format("Mon 2 Jan at 15:04")
	return []string{c.CustomerName, c.ServiceName, c.BusinessName, when, c.ManageURL}
}

// Body renders the free-form text, used when no approved template is set.
func (c MessageContent) Body(t MessageType) string {
	v := c.Vars(t)
	if t == MessageReview {
		return fmt.Sprintf("Thanks for visiting %[2]s, %[1]s! If you have a minute, a quick review would mean a lot: %[3]s", v[0], v[1], v[2])
	}
	return fmt.Sprintf("Hi %s, this is a reminder of your %s at %s on %s. Need to cancel? %s", v[0], v[1], v[2], v[3], v[4])
}
