// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// The upstream Flight SQL driver installs a default logger (JSON to stderr
// at ERROR) and logs client-initiated stream cancellations — e.g. an ADBC
// consumer closing a DoGet stream early after probing the schema, as
// DuckDB's adbc_scanner and Columnar's adbc extension both do — as errors
// ("FlightSQL endpoint DoGet failed ... context canceled"). Those are
// expected and not actionable, so the driver's loggers suppress them.

// cancelFilterHandler wraps a slog.Handler and drops records whose err
// attribute reports a context cancellation.
type cancelFilterHandler struct {
	slog.Handler
}

func (h cancelFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	canceled := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "err" || a.Key == "error" {
			if strings.Contains(a.Value.String(), "context canceled") {
				canceled = true
				return false
			}
		}
		return true
	})
	if canceled {
		return nil
	}
	return h.Handler.Handle(ctx, r)
}

func (h cancelFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return cancelFilterHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h cancelFilterHandler) WithGroup(name string) slog.Handler {
	return cancelFilterHandler{Handler: h.Handler.WithGroup(name)}
}

// NewLogger returns the driver's standard logger: JSON to stderr at the
// given level, with expected client-cancellation errors suppressed. It is
// installed as the default database logger and used by the C shared
// library's log-level environment handling.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(cancelFilterHandler{Handler: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: false,
		Level:     level,
	})})
}
