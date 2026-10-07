// Package logging configures the default slog logger.
package logging

import (
	"log/slog"
	"os"
)

// Setup logs readable text in dev and JSON (for log search) in prod.
func Setup(env string) {
	var h slog.Handler
	if env == "dev" {
		h = slog.NewTextHandler(os.Stdout, nil)
	} else {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(h))
}
