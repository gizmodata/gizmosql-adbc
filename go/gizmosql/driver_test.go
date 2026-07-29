// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc"
)

// recordingDriver is a fake flightsql.Driver that records the options
// passed to it, so tests can assert what the wrapper hands downstream.
type recordingDriver struct {
	gotOpts map[string]string
}

func (r *recordingDriver) NewDatabase(opts map[string]string) (adbc.Database, error) {
	return r.NewDatabaseWithOptions(opts)
}

func (r *recordingDriver) NewDatabaseWithContext(
	ctx context.Context, opts map[string]string,
) (adbc.Database, error) {
	return r.NewDatabaseWithOptionsContext(ctx, opts)
}

func (r *recordingDriver) NewDatabaseWithOptions(
	opts map[string]string, dialOpts ...grpc.DialOption,
) (adbc.Database, error) {
	return r.NewDatabaseWithOptionsContext(context.Background(), opts, dialOpts...)
}

func (r *recordingDriver) NewDatabaseWithOptionsContext(
	_ context.Context, opts map[string]string, _ ...grpc.DialOption,
) (adbc.Database, error) {
	r.gotOpts = opts
	return &recordingDatabase{}, nil
}

type recordingDatabase struct {
	gotOpts map[string]string
}

func (r *recordingDatabase) SetOptions(opts map[string]string) error {
	r.gotOpts = opts
	return nil
}

func (r *recordingDatabase) Open(context.Context) (adbc.Connection, error) {
	return nil, nil
}

func (r *recordingDatabase) Close() error { return nil }

func TestNewDatabaseRewritesGizmoSQLScheme(t *testing.T) {
	rec := &recordingDriver{}
	drv := &driverImpl{inner: rec}
	db, err := drv.NewDatabase(map[string]string{
		"uri":      "gizmosql://localhost:31337?transport=tcp",
		"username": "u",
	})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.Close()

	if got, want := rec.gotOpts["uri"], "flightsql://localhost:31337?transport=tcp"; got != want {
		t.Errorf("downstream uri = %q, want %q", got, want)
	}
	if rec.gotOpts["username"] != "u" {
		t.Errorf("unrelated option lost: %q", rec.gotOpts["username"])
	}
}

func TestSetOptionsRewritesGizmoSQLScheme(t *testing.T) {
	rec := &recordingDriver{}
	drv := &driverImpl{inner: rec}
	db, err := drv.NewDatabase(map[string]string{"uri": "grpc+tls://localhost:31337"})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.Close()

	if err := db.SetOptions(map[string]string{"uri": "gizmosql://h:1"}); err != nil {
		t.Fatalf("SetOptions: %v", err)
	}
	innerDB := db.(*database).Database.(*recordingDatabase)
	if got, want := innerDB.gotOpts["uri"], "flightsql://h:1"; got != want {
		t.Errorf("downstream uri = %q, want %q", got, want)
	}
}

// TestNewDatabaseRealDriver exercises the real upstream driver end to
// end for option validation (no server contact happens at this stage).
func TestNewDatabaseRealDriver(t *testing.T) {
	drv := NewDriver(memory.DefaultAllocator)
	for _, uri := range []string{
		"gizmosql://localhost:31337",
		"gizmosql://localhost:31337?transport=tcp",
		"flightsql://localhost:31337",
		"grpc+tls://localhost:31337",
		"grpc+tcp://localhost:31337",
	} {
		db, err := drv.NewDatabase(map[string]string{"uri": uri})
		if err != nil {
			t.Fatalf("NewDatabase(%q) failed: %v", uri, err)
		}
		db.Close()
	}
}
