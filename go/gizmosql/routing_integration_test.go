// Licensed under the Apache License, Version 2.0.

// Live-server tests for GizmoSQL's lazy-execution workaround: DDL/DML
// issued through ExecuteQuery must actually execute even when the caller
// never consumes the result — parity with the 1.x Python driver's
// TestExecuteAutoDetect / TestReturningClause integration tests.

package gizmosql

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func openConn(t *testing.T, port int) adbc.Connection {
	t.Helper()
	drv := NewDriver(memory.DefaultAllocator)
	db, err := drv.NewDatabase(connectOptions(port))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cnxn, err := db.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cnxn.Close() })
	return cnxn
}

// execQuery runs sql via ExecuteQuery WITHOUT consuming the result —
// exactly the pattern that GizmoSQL's lazy planning would no-op without
// the driver's routing. Returns the affected-row count.
func execQuery(t *testing.T, cnxn adbc.Connection, sql string) int64 {
	t.Helper()
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatalf("NewStatement: %v", err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(sql); err != nil {
		t.Fatalf("SetSqlQuery(%q): %v", sql, err)
	}
	reader, affected, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery(%q): %v", sql, err)
	}
	reader.Release() // deliberately never read
	return affected
}

func queryInt64(t *testing.T, cnxn adbc.Connection, sql string) int64 {
	t.Helper()
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatalf("NewStatement: %v", err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(sql); err != nil {
		t.Fatalf("SetSqlQuery(%q): %v", sql, err)
	}
	reader, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery(%q): %v", sql, err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatalf("no rows from %q", sql)
	}
	rec := reader.RecordBatch()
	switch col := rec.Column(0).(type) {
	case *array.Int32:
		return int64(col.Value(0))
	case *array.Int64:
		return col.Value(0)
	default:
		t.Fatalf("unexpected column type %T for %q", rec.Column(0), sql)
		return 0
	}
}

func TestIntegrationDDLDMLExecutesWithoutFetch(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	execQuery(t, cnxn, "CREATE TABLE routing_t (id INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS routing_t")

	if n := execQuery(t, cnxn, "INSERT INTO routing_t VALUES (1), (2)"); n != 2 {
		t.Errorf("INSERT affected = %d, want 2", n)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM routing_t"); got != 2 {
		t.Errorf("COUNT(*) = %d, want 2 — DDL/DML did not execute server-side", got)
	}
	if n := execQuery(t, cnxn, "UPDATE routing_t SET id = 10 WHERE id = 1"); n != 1 {
		t.Errorf("UPDATE affected = %d, want 1", n)
	}
	if n := execQuery(t, cnxn, "DELETE FROM routing_t WHERE id = 10"); n != 1 {
		t.Errorf("DELETE affected = %d, want 1", n)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM routing_t"); got != 1 {
		t.Errorf("COUNT(*) after delete = %d, want 1", got)
	}
}

func TestIntegrationReturningPersistsWithoutFetch(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	execQuery(t, cnxn, "CREATE TABLE returning_t (id INT, msg VARCHAR)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS returning_t")

	// The regression at the heart of 1.x issue #3: RETURNING takes the
	// query path, and the reader is released unread — the INSERT must
	// still have fired thanks to eager materialization.
	n := execQuery(t, cnxn, "INSERT INTO returning_t VALUES (1, 'a') RETURNING id")
	if n != 1 {
		t.Errorf("INSERT..RETURNING affected = %d, want 1", n)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM returning_t"); got != 1 {
		t.Errorf("COUNT(*) = %d, want 1 — RETURNING insert did not persist", got)
	}
}

func TestIntegrationReturningYieldsRows(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	execQuery(t, cnxn, "CREATE TABLE returning_rows_t (id INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS returning_rows_t")

	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatalf("NewStatement: %v", err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(
		"INSERT INTO returning_rows_t VALUES (41), (42) RETURNING id"); err != nil {
		t.Fatal(err)
	}
	reader, affected, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	defer reader.Release()

	if affected != 2 {
		t.Errorf("affected = %d, want 2", affected)
	}
	var got []int64
	for reader.Next() {
		rec := reader.RecordBatch()
		switch col := rec.Column(0).(type) {
		case *array.Int32:
			for i := 0; i < int(rec.NumRows()); i++ {
				got = append(got, int64(col.Value(i)))
			}
		case *array.Int64:
			for i := 0; i < int(rec.NumRows()); i++ {
				got = append(got, col.Value(i))
			}
		default:
			t.Fatalf("unexpected column type %T", rec.Column(0))
		}
	}
	if len(got) != 2 || got[0] != 41 || got[1] != 42 {
		t.Errorf("RETURNING rows = %v, want [41 42]", got)
	}
}

func TestIntegrationStringLiteralReturningNotMisrouted(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	execQuery(t, cnxn, "CREATE TABLE literal_t (msg VARCHAR)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS literal_t")

	// 'returning' as a value: classified as plain DML, executes via DoPut.
	if n := execQuery(t, cnxn, "INSERT INTO literal_t VALUES ('returning')"); n != 1 {
		t.Errorf("affected = %d, want 1", n)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM literal_t"); got != 1 {
		t.Errorf("COUNT(*) = %d, want 1", got)
	}
}

func TestIntegrationSelectStillStreams(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	// SELECT must not be routed through ExecuteUpdate: verify a normal
	// result set arrives with the expected value.
	if got := queryInt64(t, cnxn, "SELECT 41 + 1"); got != 42 {
		t.Errorf("SELECT 41 + 1 = %d, want 42", got)
	}
}

func TestIntegrationConnectionOptionsDelegate(t *testing.T) {
	// Regression: the wrappers must expose the upstream driver's option
	// interfaces (dbt-gizmosql sets adbc.connection.catalog at connect
	// time and reads it back via adbc_current_catalog).
	port := startServer(t)
	cnxn := openConn(t, port)

	opts, ok := cnxn.(adbc.GetSetOptions)
	if !ok {
		t.Fatal("connection wrapper does not expose adbc.GetSetOptions")
	}
	// dbt's flow: set the catalog at connect time, read it back later via
	// adbc_current_catalog. (A get without a prior set is a server-side
	// session error, unrelated to the wrapper.)
	if err := opts.SetOption("adbc.connection.catalog", "memory"); err != nil {
		t.Fatalf("SetOption(adbc.connection.catalog): %v", err)
	}
	catalog, err := opts.GetOption("adbc.connection.catalog")
	if err != nil {
		t.Fatalf("GetOption(adbc.connection.catalog): %v", err)
	}
	if catalog != "memory" {
		t.Errorf("current catalog = %q, want memory", catalog)
	}

	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if _, ok := stmt.(adbc.GetSetOptions); !ok {
		t.Error("statement wrapper does not expose adbc.GetSetOptions")
	}
}

func TestIntegrationDDLDMLRoutingAfterBind(t *testing.T) {
	// Regression (found via sqlmesh-gizmosql): Bind must not permanently
	// disable DDL/DML routing on a reused statement. Python's dbapi
	// cursor keeps one ADBC statement for its lifetime and adbc_ingest
	// binds on it — a later plain INSERT on the same statement was
	// silently no-oped under lazy execution.
	port := startServer(t)
	cnxn := openConn(t, port)
	ctx := context.Background()

	execQuery(t, cnxn, "CREATE TABLE bindreset_t (id INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS bindreset_t")

	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	// Step 1: a bulk ingest on this statement — exactly what Python's
	// cursor.adbc_ingest() does (ingest options + Bind + ExecuteUpdate).
	if err := stmt.SetOption(adbc.OptionKeyIngestTargetTable, "bindreset_src"); err != nil {
		t.Fatalf("set ingest target: %v", err)
	}
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int32}}, nil)
	bldr := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	bldr.Field(0).(*array.Int32Builder).AppendValues([]int32{7, 8, 9}, nil)
	rec := bldr.NewRecordBatch()
	bldr.Release()
	if err := stmt.Bind(ctx, rec); err != nil {
		rec.Release()
		t.Fatalf("Bind: %v", err)
	}
	rec.Release()
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatalf("ingest ExecuteUpdate: %v", err)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM bindreset_src"); got != 3 {
		t.Fatalf("ingest COUNT(*) = %d, want 3", got)
	}

	// Step 2: plain DDL/DML on the SAME statement, result never read —
	// must still execute immediately.
	if err := stmt.SetSqlQuery("INSERT INTO bindreset_t VALUES (1), (2)"); err != nil {
		t.Fatal(err)
	}
	reader, affected, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("INSERT after bind: %v", err)
	}
	reader.Release()
	if affected != 2 {
		t.Errorf("INSERT affected = %d, want 2 (routing disabled after Bind?)", affected)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM bindreset_t"); got != 2 {
		t.Errorf("COUNT(*) = %d, want 2 — INSERT after Bind was silently lost", got)
	}
}

// wkbPoint returns the little-endian WKB encoding of POINT(x y).
func wkbPoint(x, y float64) []byte {
	buf := make([]byte, 21)
	buf[0] = 1 // little endian
	binary.LittleEndian.PutUint32(buf[1:5], 1)
	binary.LittleEndian.PutUint64(buf[5:13], math.Float64bits(x))
	binary.LittleEndian.PutUint64(buf[13:21], math.Float64bits(y))
	return buf
}

func geoRecord(t *testing.T, xs, ys []float64) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int32},
		{Name: "geom", Type: arrow.BinaryTypes.Binary, Nullable: true,
			Metadata: arrow.NewMetadata(
				[]string{"ARROW:extension:name", "ARROW:extension:metadata"},
				[]string{"geoarrow.wkb", "{}"})},
	}, nil)
	bldr := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer bldr.Release()
	for i := range xs {
		bldr.Field(0).(*array.Int32Builder).Append(int32(i + 1))
		bldr.Field(1).(*array.BinaryBuilder).Append(wkbPoint(xs[i], ys[i]))
	}
	return bldr.NewRecordBatch()
}

func ingestGeo(t *testing.T, cnxn adbc.Connection, table, mode string, rec arrow.RecordBatch) int64 {
	t.Helper()
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetOption(adbc.OptionKeyIngestTargetTable, table); err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		if err := stmt.SetOption(adbc.OptionKeyIngestMode, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Bind(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	n, err := stmt.ExecuteUpdate(context.Background())
	if err != nil {
		t.Fatalf("geo ingest (%s): %v", mode, err)
	}
	return n
}

func queryStr(t *testing.T, cnxn adbc.Connection, sql string) string {
	t.Helper()
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(sql); err != nil {
		t.Fatal(err)
	}
	reader, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery(%q): %v", sql, err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatalf("no rows from %q", sql)
	}
	return reader.RecordBatch().Column(0).ValueStr(0)
}

func TestIntegrationGeoArrowIngest(t *testing.T) {
	// Regression for gizmodata/adbc-driver-gizmosql#5: geoarrow.wkb
	// columns must ingest as GEOMETRY, not BLOB — in every mode.
	port := startServer(t)
	cnxn := openConn(t, port)
	rec := geoRecord(t, []float64{0, 1}, []float64{0, 1})
	defer rec.Release()

	// create: table must come out with a GEOMETRY column
	if n := ingestGeo(t, cnxn, "geo_create", "", rec); n != 2 {
		t.Errorf("create ingest rows = %d, want 2", n)
	}
	typ := queryStr(t, cnxn,
		"SELECT column_type FROM (DESCRIBE geo_create) WHERE column_name = 'geom'")
	if typ != "GEOMETRY" {
		t.Errorf("create-mode geom type = %q, want GEOMETRY", typ)
	}
	if wkt := queryStr(t, cnxn,
		"SELECT st_astext(geom) FROM geo_create WHERE id = 2"); wkt != "POINT (1 1)" {
		t.Errorf("round-tripped value = %q, want POINT (1 1)", wkt)
	}

	// append into an existing GEOMETRY table (the reporter's second repro)
	execQuery(t, cnxn, "CREATE TABLE geo_append (id INT, geom GEOMETRY)")
	if n := ingestGeo(t, cnxn, "geo_append", adbc.OptionValueIngestModeAppend, rec); n != 2 {
		t.Errorf("append ingest rows = %d, want 2", n)
	}
	if cnt := queryStr(t, cnxn, "SELECT COUNT(*) FROM geo_append"); cnt != "2" {
		t.Errorf("append count = %s, want 2", cnt)
	}

	// replace over the created table
	if n := ingestGeo(t, cnxn, "geo_create", adbc.OptionValueIngestModeReplace, rec); n != 2 {
		t.Errorf("replace ingest rows = %d, want 2", n)
	}

	// create_append: once into a fresh name, once more to append
	ingestGeo(t, cnxn, "geo_ca", adbc.OptionValueIngestModeCreateAppend, rec)
	ingestGeo(t, cnxn, "geo_ca", adbc.OptionValueIngestModeCreateAppend, rec)
	if cnt := queryStr(t, cnxn, "SELECT COUNT(*) FROM geo_ca"); cnt != "4" {
		t.Errorf("create_append count = %s, want 4", cnt)
	}

	// non-geometry ingest still takes the plain path
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int32}}, nil)
	bldr := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	bldr.Field(0).(*array.Int32Builder).Append(7)
	plain := bldr.NewRecordBatch()
	bldr.Release()
	defer plain.Release()
	if n := ingestGeo(t, cnxn, "plain_t", "", plain); n != 1 {
		t.Errorf("plain ingest rows = %d, want 1", n)
	}
	if typ := queryStr(t, cnxn,
		"SELECT column_type FROM (DESCRIBE plain_t) WHERE column_name = 'v'"); typ != "INTEGER" {
		t.Errorf("plain column type = %q", typ)
	}
}
