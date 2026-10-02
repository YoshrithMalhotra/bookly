package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YoshrithMalhotra/bookly/internal/config"
	"github.com/YoshrithMalhotra/bookly/internal/notify"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var sender notify.Sender = notify.FakeSender{}

	ticker := time.NewTicker(cfg.WorkerInterval)
	defer ticker.Stop()

	slog.Info("worker started", "interval", cfg.WorkerInterval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped")
			return
		case <-ticker.C:
			sendDue(ctx, sender)
		}
	}
}

// sendDue is the heart of the product.
// TODO(week 2):
//  1. SELECT due pending messages ... FOR UPDATE SKIP LOCKED LIMIT n
//  2. Skip/cancel if the appointment was cancelled (and for reviews, isn't 'done')
//  3. sender.Send, then mark sent — or record the error and retry later
func sendDue(ctx context.Context, sender notify.Sender) {
	_ = sender
}
