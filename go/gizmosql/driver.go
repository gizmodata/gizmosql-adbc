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
	"sync"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-adbc/go/adbc/driver/flightsql"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
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
// statements and to serialize GetInfo calls (the upstream driver's
// DriverInfo map is not safe for concurrent use; see
// apache/arrow-adbc#1178 and the 1.x Python driver's adbc_get_info fix).
type connection struct {
	adbc.Connection
	getInfoMu sync.Mutex
}

func (c *connection) NewStatement() (adbc.Statement, error) {
	stmt, err := c.Connection.NewStatement()
	if err != nil {
		return nil, err
	}
	return &statement{Statement: stmt}, nil
}

func (c *connection) GetInfo(ctx context.Context, infoCodes []adbc.InfoCode) (array.RecordReader, error) {
	c.getInfoMu.Lock()
	defer c.getInfoMu.Unlock()
	return c.Connection.GetInfo(ctx, infoCodes)
}

// statement wraps the upstream statement with GizmoSQL's execution
// routing, working around the server's lazy-execution model (the
// GetFlightInfo RPC only *plans* a statement; execution normally happens
// on DoGet, which a caller that never fetches would never trigger):
//
//   - DDL/DML (by first keyword, comments stripped) executes immediately
//     via ExecuteUpdate (DoPut) even when invoked through ExecuteQuery,
//     returning an empty result set plus the affected-row count.
//   - INSERT/UPDATE/DELETE ... RETURNING takes the query path but is
//     eagerly materialized, so the DML fires regardless of whether the
//     caller consumes the returned reader.
//   - Everything else (SELECT/WITH/SHOW/...) streams as usual.
//
// Routing applies only to plain SQL without bound parameters, matching
// the 1.x Python driver (Bind switches to prepared-statement semantics).
type statement struct {
	adbc.Statement
	query    string // last SetSqlQuery value; "" for substrait plans
	hasBound bool
}

func (s *statement) SetSqlQuery(query string) error {
	if err := s.Statement.SetSqlQuery(query); err != nil {
		return err
	}
	s.query = query
	return nil
}

func (s *statement) SetSubstraitPlan(plan []byte) error {
	if err := s.Statement.SetSubstraitPlan(plan); err != nil {
		return err
	}
	s.query = ""
	return nil
}

func (s *statement) Bind(ctx context.Context, values arrow.RecordBatch) error {
	if err := s.Statement.Bind(ctx, values); err != nil {
		return err
	}
	s.hasBound = true
	return nil
}

func (s *statement) BindStream(ctx context.Context, stream array.RecordReader) error {
	if err := s.Statement.BindStream(ctx, stream); err != nil {
		return err
	}
	s.hasBound = true
	return nil
}

func (s *statement) ExecuteQuery(ctx context.Context) (array.RecordReader, int64, error) {
	if s.query == "" || s.hasBound {
		return s.Statement.ExecuteQuery(ctx)
	}
	if isDDLDML(s.query) {
		affected, err := s.Statement.ExecuteUpdate(ctx)
		if err != nil {
			return nil, -1, err
		}
		reader, err := array.NewRecordReader(emptySchema, nil)
		if err != nil {
			return nil, -1, err
		}
		return reader, affected, nil
	}
	reader, affected, err := s.Statement.ExecuteQuery(ctx)
	if err != nil {
		return nil, -1, err
	}
	if hasReturningClause(stripSQLComments(s.query)) {
		return materialize(reader, affected)
	}
	return reader, affected, nil
}

var emptySchema = arrow.NewSchema([]arrow.Field{}, nil)

// materialize drains reader into memory and returns an equivalent
// in-memory reader, guaranteeing the server-side statement actually ran.
// The affected-row count is replaced by the materialized row count when
// the driver reported it as unknown (-1).
func materialize(reader array.RecordReader, affected int64) (array.RecordReader, int64, error) {
	defer reader.Release()
	var (
		recs []arrow.RecordBatch
		rows int64
	)
	releaseAll := func() {
		for _, rec := range recs {
			rec.Release()
		}
	}
	for reader.Next() {
		rec := reader.RecordBatch()
		rec.Retain()
		recs = append(recs, rec)
		rows += rec.NumRows()
	}
	if err := reader.Err(); err != nil {
		releaseAll()
		return nil, -1, err
	}
	cached, err := array.NewRecordReader(reader.Schema(), recs)
	if err != nil {
		releaseAll()
		return nil, -1, err
	}
	// NewRecordReader retained the records; drop our references.
	releaseAll()
	if affected < 0 {
		affected = rows
	}
	return cached, affected, nil
}
