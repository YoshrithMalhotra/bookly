package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/booking"
	"github.com/YoshrithMalhotra/bookly/internal/store"
	"github.com/YoshrithMalhotra/bookly/internal/testdb"
	"github.com/YoshrithMalhotra/bookly/migrations"
)

func TestOverlapIsSlotTakenWithNoOrphanMessages(t *testing.T) {
	ctx := context.Background()
	s := testdb.New(t)
	b, err := s.CreateBusiness(ctx, store.NewBusiness{Name: "S", Slug: "s", Timezone: "UTC", OwnerEmail: "s@example.com", PasswordHash: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	sv, _ := s.CreateService(ctx, b.ID, store.Service{Name: "Cut", DurationMin: 60, Price: "1", Active: true})

	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	msgs := booking.PlanMessages(start, start.Add(time.Hour), time.Now(), true)
	req := booking.Request{ServiceID: sv.ID, StartsAt: start, CustomerName: "A", CustomerPhone: "+447700900123", WhatsAppOptIn: true}
	if _, _, err := s.CreateAppointment(ctx, b.ID, req, start.Add(time.Hour), msgs, store.SourceOnline); err != nil {
		t.Fatal(err)
	}

	req.StartsAt = start.Add(30 * time.Minute)
	_, _, err = s.CreateAppointment(ctx, b.ID, req, req.StartsAt.Add(time.Hour), msgs, store.SourceOnline)
	if !errors.Is(err, booking.ErrSlotTaken) {
		t.Fatalf("got %v, want ErrSlotTaken", err)
	}
	// Only the first appointment's 2 messages exist.
	total := 0
	for id := int64(1); id <= 5; id++ {
		m, _ := s.Messages(ctx, b.ID, id)
		total += len(m)
	}
	if total != 2 {
		t.Errorf("found %d messages, want 2 (no orphans)", total)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := testdb.New(t)
	if err := s.Migrate(context.Background(), migrations.FS); err != nil {
		t.Fatal(err)
	}
}
