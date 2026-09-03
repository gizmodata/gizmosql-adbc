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
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

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
	resolved, err := applyGizmoSQLOptions(ctx, rewriteOptions(opts))
	if err != nil {
		return nil, err
	}
	// Interceptors that let the wrapper observe the per-session gRPC
	// connection, over which server-side cancels are issued; see cancel.go.
	dialOpts = append(append([]grpc.DialOption{}, dialOpts...), captureDialOptions()...)
	db, err := d.inner.NewDatabaseWithOptionsContext(ctx, resolved, dialOpts...)
	if err != nil {
		return nil, err
	}
	// Replace the upstream default logger (which reports expected
	// client-initiated stream cancellations at ERROR) with the filtered
	// one; see logging.go.
	if lg, ok := db.(adbc.DatabaseLogging); ok {
		lg.SetLogger(NewLogger(slog.LevelError))
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

// SetLogger forwards to the upstream database. The wrapper embeds the
// adbc.Database interface, which does not include SetLogger, so without
// this explicit forwarder an adbc.DatabaseLogging assertion on the
// wrapper fails and callers (e.g. the C shared library's log-level env
// handling) cannot replace the logger.
func (db *database) SetLogger(logger *slog.Logger) {
	if lg, ok := db.Database.(adbc.DatabaseLogging); ok {
		lg.SetLogger(logger)
	}
}

func (db *database) Open(ctx context.Context) (adbc.Connection, error) {
	// Upstream authenticates and issues its first RPCs during Open; the
	// canceler rides along in the context so the interceptors installed by
	// captureDialOptions can record the session's connection.
	sc := &sessionCanceler{}
	conn, err := db.Database.Open(withSessionCanceler(ctx, sc))
	if err != nil {
		return nil, err
	}
	return &connection{Connection: conn, canceler: sc}, nil
}

// connection wraps the upstream connection to hand out GizmoSQL
// statements and to serialize GetInfo calls (the upstream driver's
// DriverInfo map is not safe for concurrent use; see
// apache/arrow-adbc#1178 and the 1.x Python driver's adbc_get_info fix).
type connection struct {
	adbc.Connection
	getInfoMu sync.Mutex
	canceler  *sessionCanceler
}

func (c *connection) NewStatement() (adbc.Statement, error) {
	stmt, err := c.Connection.NewStatement()
	if err != nil {
		return nil, err
	}
	return &statement{Statement: stmt, cnxn: c.Connection, canceler: c.canceler}, nil
}

// CancelQuery asks the server to interrupt whatever statement this
// connection's session is currently executing (see cancel.go).
func (c *connection) CancelQuery(ctx context.Context) error {
	return c.canceler.cancel(ctx, "")
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
	cnxn     adbc.Connection // for transparent statement recreation
	query    string          // last SetSqlQuery value; "" for substrait plans
	hasBound bool
	// prepared reports whether Prepare has run on the current inner
	// statement. Bind/BindStream on a SQL query auto-prepare when it is
	// false: upstream treats an unprepared Bind as staged bulk-ingest
	// data and then rejects execution ("must set IngestTargetTable
	// before bulk ingestion"), which breaks consumers that have no
	// Prepare call at all (e.g. the Node.js adbc-driver-manager).
	prepared bool
	// setOpts records options applied to the inner statement so they can
	// be replayed if the statement is recreated (per-operation
	// adbc.ingest.* keys excluded).
	setOpts []statementOption
	// Ingest state, tracked for the geometry-aware ingest path
	// (geoingest.go). Mirrors the options forwarded to the inner
	// statement.
	ingestTarget   string
	ingestMode     string
	ingestCatalog  string
	ingestDBSchema string
	ingestTemp     bool
	boundSchema    *arrow.Schema
	// Server-side cancellation (cancel.go): the session canceler shared
	// with the parent connection, and the state of the most recent
	// streaming execution so Close can interrupt an abandoned query.
	canceler *sessionCanceler
	current  *execState
	// inFlight is the blocking execute call currently in progress
	// (ExecuteUpdate, or ExecuteQuery up to the point it returns a
	// reader), if any, so Close/CancelQuery can interrupt it on the server
	// even though the caller's goroutine is parked inside the RPC. Without
	// this, releasing a statement while a DoPut update ran left the update
	// running to completion.
	inFlight atomic.Pointer[callState]
}

// callState tracks one blocking execute call so the server is asked to
// interrupt it at most once.
type callState struct {
	cancelled atomic.Bool
}

// beginCall marks a blocking execute call as in progress.
func (s *statement) beginCall() *callState {
	c := &callState{}
	s.inFlight.Store(c)
	return c
}

// endCall clears the in-flight marker once the blocking call returns.
func (s *statement) endCall(c *callState) {
	s.inFlight.CompareAndSwap(c, nil)
}

// claimInFlight reports whether a blocking call is in progress that has
// not been cancelled yet, claiming the cancel so it is sent only once.
func (s *statement) claimInFlight() bool {
	c := s.inFlight.Load()
	return c != nil && c.cancelled.CompareAndSwap(false, true)
}

// CancelQuery asks the server to interrupt the statement's in-flight
// query. It may be called concurrently with a blocked ExecuteQuery or
// ExecuteUpdate; that call then returns the server's interrupt error.
func (s *statement) CancelQuery(ctx context.Context) error {
	if s.canceler == nil {
		return errNoCanceler
	}
	if cur := s.current; cur != nil {
		cur.cancelled.Store(true)
	}
	if c := s.inFlight.Load(); c != nil {
		c.cancelled.Store(true)
	}
	return s.canceler.cancel(ctx, s.query)
}

// cancelAbandoned best-effort interrupts an execution the caller walked
// away from before draining it (statement closed, reader released).
func (s *statement) cancelAbandoned() {
	if s.canceler == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), implicitCancelTimeout)
	defer cancel()
	_ = s.canceler.cancel(ctx, s.query)
}

// Close interrupts an abandoned execution on the server before closing
// the inner statement: either a blocking ExecuteUpdate/ExecuteQuery call
// another goroutine is still parked in (e.g. a language binding releasing
// the statement to cancel it), or a streaming result that was never
// drained.
func (s *statement) Close() error {
	if s.claimInFlight() {
		s.cancelAbandoned()
	} else if cur := s.current; cur != nil && cur.abandoned() {
		s.cancelAbandoned()
	}
	return s.Statement.Close()
}

type statementOption struct {
	key   string
	value any // string, []byte, int64, or float64
}

func (s *statement) recordOption(key string, value any) {
	if strings.HasPrefix(key, "adbc.ingest.") {
		return
	}
	s.setOpts = append(s.setOpts, statementOption{key: key, value: value})
}

// resetForNewQuery discards stale bound data by recreating the inner
// statement. The upstream Flight SQL driver leaves s.bound/s.streamBind
// staged after a completed bulk ingest, and (since arrow-adbc 1.12)
// rejects any later plain-SQL execution on that statement with "must
// set IngestTargetTable before bulk ingestion". Python's dbapi cursor
// reuses one ADBC statement for its whole life (cursor.adbc_ingest then
// cursor.execute), so without this reset the statement would be
// permanently poisoned — and the sticky bound flag would also disable
// DDL/DML routing, silently no-oping INSERT/COMMIT under GizmoSQL's
// lazy execution (found via the sqlmesh-gizmosql test suite).
func (s *statement) resetForNewQuery() error {
	if !s.hasBound {
		return nil
	}
	fresh, err := s.cnxn.NewStatement()
	if err != nil {
		return err
	}
	for _, opt := range s.setOpts {
		switch v := opt.value.(type) {
		case string:
			err = fresh.SetOption(opt.key, v)
		case []byte:
			if o, ok := fresh.(adbc.GetSetOptions); ok {
				err = o.SetOptionBytes(opt.key, v)
			}
		case int64:
			if o, ok := fresh.(adbc.GetSetOptions); ok {
				err = o.SetOptionInt(opt.key, v)
			}
		case float64:
			if o, ok := fresh.(adbc.GetSetOptions); ok {
				err = o.SetOptionDouble(opt.key, v)
			}
		}
		if err != nil {
			fresh.Close()
			return err
		}
	}
	old := s.Statement
	s.Statement = fresh
	s.hasBound = false
	s.prepared = false
	s.boundSchema = nil
	return old.Close()
}

func (s *statement) SetOption(key, value string) error {
	if err := s.Statement.SetOption(key, value); err != nil {
		return err
	}
	s.recordOption(key, value)
	switch key {
	case adbc.OptionKeyIngestTargetTable:
		s.ingestTarget = value
	case adbc.OptionKeyIngestMode:
		s.ingestMode = value
	case adbc.OptionValueIngestTargetCatalog:
		s.ingestCatalog = value
	case adbc.OptionValueIngestTargetDBSchema:
		s.ingestDBSchema = value
	case adbc.OptionValueIngestTemporary:
		s.ingestTemp = value == adbc.OptionValueEnabled
	}
	return nil
}

func (s *statement) SetSqlQuery(query string) error {
	if err := s.resetForNewQuery(); err != nil {
		return err
	}
	if err := s.Statement.SetSqlQuery(query); err != nil {
		return err
	}
	s.query = query
	s.prepared = false // upstream closes its prepared statement here
	// A SQL query supersedes any pending ingest (mirrors the upstream
	// driver clearing its ingest target on SetSqlQuery).
	s.ingestTarget = ""
	return nil
}

func (s *statement) SetSubstraitPlan(plan []byte) error {
	if err := s.resetForNewQuery(); err != nil {
		return err
	}
	if err := s.Statement.SetSubstraitPlan(plan); err != nil {
		return err
	}
	s.query = ""
	s.prepared = false // upstream closes its prepared statement here
	return nil
}

func (s *statement) Prepare(ctx context.Context) error {
	if err := s.Statement.Prepare(ctx); err != nil {
		return err
	}
	s.prepared = true
	return nil
}

// autoPrepare makes Prepare implicit for parameter binding: a Bind or
// BindStream against a SQL query that has not been prepared prepares it
// first, so the bound data is treated as query parameters rather than
// staged bulk-ingest rows. Ingest (target table set) and Substrait
// paths are left untouched.
func (s *statement) autoPrepare(ctx context.Context) error {
	if s.query == "" || s.ingestTarget != "" || s.prepared {
		return nil
	}
	return s.Prepare(ctx)
}

func (s *statement) Bind(ctx context.Context, values arrow.RecordBatch) error {
	if err := s.autoPrepare(ctx); err != nil {
		return err
	}
	if err := s.Statement.Bind(ctx, values); err != nil {
		return err
	}
	s.hasBound = true
	s.boundSchema = values.Schema()
	return nil
}

func (s *statement) BindStream(ctx context.Context, stream array.RecordReader) error {
	if err := s.autoPrepare(ctx); err != nil {
		return err
	}
	if err := s.Statement.BindStream(ctx, stream); err != nil {
		return err
	}
	s.hasBound = true
	s.boundSchema = stream.Schema()
	return nil
}

func (s *statement) ExecuteUpdate(ctx context.Context) (int64, error) {
	call := s.beginCall()
	defer s.endCall(call)
	// Geometry-aware ingest: geoarrow.* columns need the interim-table
	// path (see geoingest.go); everything else delegates untouched.
	if s.ingestTarget != "" {
		if geoCols := geoFieldNames(s.boundSchema); len(geoCols) > 0 {
			return s.executeGeoIngest(ctx, geoCols)
		}
	}
	return s.Statement.ExecuteUpdate(ctx)
}

func (s *statement) ExecuteQuery(ctx context.Context) (array.RecordReader, int64, error) {
	call := s.beginCall()
	defer s.endCall(call)
	if s.query == "" || s.hasBound {
		reader, affected, err := s.Statement.ExecuteQuery(ctx)
		if err != nil {
			return nil, -1, err
		}
		return s.trackStreaming(reader), affected, nil
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
	return s.trackStreaming(reader), affected, nil
}

// trackStreaming records a new streaming execution and wraps its reader
// so abandoning it (release before exhaustion) cancels the query on the
// server. Materialized and synthetic readers are never in flight and are
// returned unwrapped.
func (s *statement) trackStreaming(reader array.RecordReader) array.RecordReader {
	state := &execState{}
	s.current = state
	return newCancelReader(reader, state, s.cancelAbandoned)
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
