// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func newBufLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(cancelFilterHandler{Handler: slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelError,
	})})
}

func TestCancelFilterSuppressesContextCanceled(t *testing.T) {
	var buf bytes.Buffer
	log := newBufLogger(&buf)

	log.ErrorContext(context.Background(), "FlightSQL endpoint DoGet failed",
		"endpointIndex", 0,
		"err", "rpc error: code = Canceled desc = context canceled",
	)
	if got := buf.String(); got != "" {
		t.Errorf("expected cancellation error to be suppressed, got: %s", got)
	}
}

func TestCancelFilterKeepsRealErrors(t *testing.T) {
	var buf bytes.Buffer
	log := newBufLogger(&buf)

	log.ErrorContext(context.Background(), "FlightSQL endpoint DoGet failed",
		"endpointIndex", 0,
		"err", "rpc error: code = Unavailable desc = connection refused",
	)
	if got := buf.String(); !strings.Contains(got, "connection refused") {
		t.Errorf("expected real error to be logged, got: %s", got)
	}
}

func TestCancelFilterSurvivesWithAttrsAndGroup(t *testing.T) {
	var buf bytes.Buffer
	log := newBufLogger(&buf).With("component", "test").WithGroup("grp")

	log.ErrorContext(context.Background(), "stream ended with error",
		"err", "context canceled")
	if got := buf.String(); got != "" {
		t.Errorf("expected suppression to survive With/WithGroup, got: %s", got)
	}
}

func TestCancelFilterSuppressesServerInterrupt(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(cancelFilterHandler{Handler: slog.NewJSONHandler(&buf, nil)})
	logger.Error("FlightSQL endpoint stream ended with error",
		"err", "rpc error: code = Unknown desc = An execution error has occurred: INTERRUPT Error: Interrupted!")
	if buf.Len() != 0 {
		t.Fatalf("expected server interrupt after cancel to be suppressed, got: %s", buf.String())
	}
}
