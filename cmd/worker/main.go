package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // timezone database baked in, so minimal images work

	"github.com/YoshrithMalhotra/bookly/internal/config"
	"github.com/YoshrithMalhotra/bookly/internal/logging"
	"github.com/YoshrithMalhotra/bookly/internal/notify"
	"github.com/YoshrithMalhotra/bookly/internal/store"
	"github.com/YoshrithMalhotra/bookly/internal/worker"
	"github.com/YoshrithMalhotra/bookly/migrations"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	logging.Setup(cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Migrate(ctx, migrations.FS); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	var sender notify.Sender = notify.FakeSender{}
	if cfg.TwilioAccountSID != "" {
		sender = &notify.TwilioSender{
			AccountSID: cfg.TwilioAccountSID,
			AuthToken:  cfg.TwilioAuthToken,
			From:       cfg.TwilioWhatsAppFrom,
			ContentSIDs: map[string]string{
				"reminder": cfg.TwilioReminderContentSID,
				"review":   cfg.TwilioReviewContentSID,
			},
		}
		slog.Info("using Twilio WhatsApp sender", "from", cfg.TwilioWhatsAppFrom)
	} else {
		if cfg.Env == "prod" {
			slog.Warn("TWILIO_ACCOUNT_SID not set: messages are only logged, not sent")
		}
	}

	w := &worker.Worker{Store: db, Sender: sender, PublicURL: cfg.PublicURL}
	slog.Info("worker started", "interval", cfg.WorkerInterval)
	w.Run(ctx, cfg.WorkerInterval)
	slog.Info("worker stopped")
}
