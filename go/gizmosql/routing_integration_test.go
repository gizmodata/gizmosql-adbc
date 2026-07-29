// Licensed under the Apache License, Version 2.0.

// Live-server tests for GizmoSQL's lazy-execution workaround: DDL/DML
// issued through ExecuteQuery must actually execute even when the caller
// never consumes the result — parity with the 1.x Python driver's
// TestExecuteAutoDetect / TestReturningClause integration tests.

package gizmosql

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
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
