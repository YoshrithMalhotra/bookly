package worker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/notify"
	"github.com/YoshrithMalhotra/bookly/internal/store"
	"github.com/YoshrithMalhotra/bookly/internal/testdb"
	"github.com/YoshrithMalhotra/bookly/internal/worker"
)

type countingSender struct {
	mu    sync.Mutex
	sent  map[string]int
	fails atomic.Int32 // fail this many times before succeeding
	perm  bool
}

func (c *countingSender) Send(ctx context.Context, m notify.Message) error {
	if c.fails.Add(-1) >= 0 {
		if c.perm {
			return &notify.PermanentError{Err: errors.New("bad number")}
		}
		return errors.New("twilio down")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sent == nil {
		c.sent = map[string]int{}
	}
	c.sent[m.To+"/"+m.Kind]++
	time.Sleep(2 * time.Millisecond) // widen the race window
	return nil
}

func setup(t *testing.T) (*store.Store, int64, int64) {
	ctx := context.Background()
	s := testdb.New(t)
	b, err := s.CreateBusiness(ctx, store.NewBusiness{Name: "Salon", Slug: "salon", Timezone: "Europe/London",
		OwnerEmail: "a@example.com", PasswordHash: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	sv, err := s.CreateService(ctx, b.ID, store.Service{Name: "Cut", DurationMin: 30, Price: "20", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	return s, b.ID, sv.ID
}

// book creates an appointment starting in startsIn with a reminder that is already due.
func book(t *testing.T, s *store.Store, businessID, serviceID int64, i int, startsIn time.Duration) int64 {
	start := time.Now().Add(startsIn).Truncate(time.Minute).Add(time.Duration(i) * time.Hour)
	id, _, err := s.CreateAppointment(context.Background(), businessID, booking.Request{
		ServiceID: serviceID, StartsAt: start, CustomerName: "C",
		CustomerPhone: "+4477009001" + string(rune('0'+i/10)) + string(rune('0'+i%10)), WhatsAppOptIn: true,
	}, start.Add(30*time.Minute), []booking.PlannedMessage{{Type: booking.MessageReminder, SendAt: time.Now().Add(-time.Minute)}}, store.SourceOnline)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTwoWorkersNeverSendTwice(t *testing.T) {
	s, bid, sid := setup(t)
	for i := range 40 {
		book(t, s, bid, sid, i, 3*time.Hour)
	}
	sender := &countingSender{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := &worker.Worker{Store: s, Sender: sender, PublicURL: "https://x"}
			if _, err := w.SendDue(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(sender.sent) != 40 {
		t.Errorf("sent to %d numbers, want 40", len(sender.sent))
	}
	for k, n := range sender.sent {
		if n != 1 {
			t.Errorf("%s sent %d times", k, n)
		}
	}
}

func TestRetryThenSuccess(t *testing.T) {
	s, bid, sid := setup(t)
	id := book(t, s, bid, sid, 0, 3*time.Hour)
	sender := &countingSender{}
	sender.fails.Store(2)

	clock := time.Now()
	w := &worker.Worker{Store: s, Sender: sender, PublicURL: "https://x", Now: func() time.Time { return clock }}
	ctx := context.Background()
	for range 3 {
		if _, err := w.SendDue(ctx); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Hour) // past the retry backoff
	}
	msgs, err := s.Messages(ctx, bid, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Status != "sent" || msgs[0].Attempts != 2 || msgs[0].SentAt == nil {
		t.Fatalf("got %+v", msgs)
	}
}

func TestPermanentFailure(t *testing.T) {
	s, bid, sid := setup(t)
	id := book(t, s, bid, sid, 0, 3*time.Hour)
	sender := &countingSender{perm: true}
	sender.fails.Store(1)
	w := &worker.Worker{Store: s, Sender: sender, PublicURL: "https://x"}
	w.SendDue(context.Background())
	msgs, _ := s.Messages(context.Background(), bid, id)
	if msgs[0].Status != "failed" || msgs[0].Attempts != 1 || msgs[0].LastError == nil {
		t.Fatalf("got %+v", msgs[0])
	}
}

func TestCancelledAppointmentIsNotMessaged(t *testing.T) {
	s, bid, sid := setup(t)
	id := book(t, s, bid, sid, 0, 3*time.Hour)
	if _, err := s.SetStatus(context.Background(), bid, id, booking.StatusCancelled, time.Now()); err != nil {
		t.Fatal(err)
	}
	sender := &countingSender{}
	w := &worker.Worker{Store: s, Sender: sender, PublicURL: "https://x"}
	if n, _ := w.SendDue(context.Background()); n != 0 {
		t.Errorf("processed %d, want 0 (message should already be cancelled)", n)
	}
	if len(sender.sent) != 0 {
		t.Error("sent a message for a cancelled appointment")
	}
}
