// Licensed under the Apache License, Version 2.0.

// Package gizmosql is an ADBC driver for GizmoSQL, built on the Apache
// Arrow ADBC Flight SQL driver.
//
// It delegates transport, authentication, TLS, cookies, timeouts, and
// telemetry to the upstream driver, and layers on GizmoSQL-specific
// behavior:
//
//   - the gizmosql:// URI scheme (TLS by default; ?transport=tcp for
//     plaintext), rewritten onto the upstream flightsql:// scheme
//   - (planned) DDL/DML auto-detection with immediate server-side
//     execution, working around GizmoSQL's lazy-execution model
//   - (planned) OAuth/SSO code-exchange authentication
package gizmosql

import (
	"context"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-adbc/go/adbc/driver/flightsql"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc"
)

// Driver is the extended adbc.Driver interface for GizmoSQL, mirroring
// the upstream flightsql.Driver extension methods for gRPC dial options.
type Driver interface {
	adbc.Driver
	adbc.DriverWithContext
	NewDatabaseWithOptions(map[string]string, ...grpc.DialOption) (adbc.Database, error)
	NewDatabaseWithOptionsContext(context.Context, map[string]string, ...grpc.DialOption) (adbc.Database, error)
}

// NewDriver creates a new GizmoSQL ADBC driver using the given Arrow
// allocator.
func NewDriver(alloc memory.Allocator) Driver {
	return &driverImpl{inner: flightsql.NewDriver(alloc)}
}

type driverImpl struct {
	inner flightsql.Driver
}

func (d *driverImpl) NewDatabase(opts map[string]string) (adbc.Database, error) {
	return d.NewDatabaseWithOptions(opts)
}

func (d *driverImpl) NewDatabaseWithContext(ctx context.Context, opts map[string]string) (adbc.Database, error) {
	return d.NewDatabaseWithOptionsContext(ctx, opts)
}

func (d *driverImpl) NewDatabaseWithOptions(
	opts map[string]string, dialOpts ...grpc.DialOption,
) (adbc.Database, error) {
	return d.NewDatabaseWithOptionsContext(context.Background(), opts, dialOpts...)
}

func (d *driverImpl) NewDatabaseWithOptionsContext(
	ctx context.Context, opts map[string]string, dialOpts ...grpc.DialOption,
) (adbc.Database, error) {
	db, err := d.inner.NewDatabaseWithOptionsContext(ctx, rewriteOptions(opts), dialOpts...)
	if err != nil {
		return nil, err
	}
	return &database{Database: db}, nil
}

// database wraps the upstream Flight SQL database so connections (and,
// transitively, statements) can be intercepted.
type database struct {
	adbc.Database
}

func (db *database) SetOptions(opts map[string]string) error {
	return db.Database.SetOptions(rewriteOptions(opts))
}

func (db *database) Open(ctx context.Context) (adbc.Connection, error) {
	conn, err := db.Database.Open(ctx)
	if err != nil {
		return nil, err
	}
	return &connection{Connection: conn}, nil
}

// connection wraps the upstream connection to hand out GizmoSQL
// statements.
type connection struct {
	adbc.Connection
}

func (c *connection) NewStatement() (adbc.Statement, error) {
	stmt, err := c.Connection.NewStatement()
	if err != nil {
		return nil, err
	}
	return &statement{Statement: stmt}, nil
}

// statement wraps the upstream statement. GizmoSQL-specific execution
// routing (DDL/DML auto-detection, RETURNING handling) will hook in
// here.
type statement struct {
	adbc.Statement
}
