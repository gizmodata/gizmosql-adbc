// Licensed under the Apache License, Version 2.0.

// Live-server regression tests for driver bugs found by the adbc-drivers.org
// validation suite (validation/ in this repository).

package gizmosql

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// A fresh connection that has executed nothing must still report its
// current catalog and schema. GizmoSQL creates sessions lazily and its
// GetSessionOptions never creates one, so before the driver opened the
// session at connect this failed with "Session not found".
func TestIntegrationCurrentCatalogOnFreshConnection(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	opts := cnxn.(adbc.GetSetOptions)
	catalog, err := opts.GetOption(adbc.OptionKeyCurrentCatalog)
	if err != nil {
		t.Fatalf("GetOption(current catalog) on a fresh connection: %v", err)
	}
	if catalog != "memory" {
		t.Errorf("current catalog = %q, want memory", catalog)
	}
	schema, err := opts.GetOption(adbc.OptionKeyCurrentDbSchema)
	if err != nil {
		t.Fatalf("GetOption(current schema) on a fresh connection: %v", err)
	}
	if schema != "main" {
		t.Errorf("current schema = %q, want main", schema)
	}
}

// The statement wrapper must expose the optional ExecuteSchema interface;
// embedding adbc.Statement hid it, so AdbcStatementExecuteSchema answered
// NOT_IMPLEMENTED.
func TestIntegrationExecuteSchemaForwarded(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	es, ok := stmt.(adbc.StatementExecuteSchema)
	if !ok {
		t.Fatal("statement wrapper does not expose adbc.StatementExecuteSchema")
	}
	if err := stmt.SetSqlQuery("SELECT 1::INTEGER AS a, 'x' AS b"); err != nil {
		t.Fatal(err)
	}
	schema, err := es.ExecuteSchema(context.Background())
	if err != nil {
		t.Fatalf("ExecuteSchema: %v", err)
	}
	if schema.NumFields() != 2 || schema.Field(0).Name != "a" || schema.Field(0).Type.ID() != arrow.INT32 ||
		schema.Field(1).Name != "b" || schema.Field(1).Type.ID() != arrow.STRING {
		t.Errorf("schema = %s, want a: int32, b: utf8", schema)
	}
}

// Bind before Prepare (legal ADBC order: SetSqlQuery, Bind, Prepare,
// ExecuteQuery) must keep the bound parameters. Bind auto-prepares, and the
// explicit Prepare used to re-prepare upstream, dropping them ("Values were
// not provided for the following prepared statement parameters").
func TestIntegrationBindThenPrepareKeepsParameters(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	stmt, counter := newCountingStatement(t, cnxn)
	if err := stmt.SetSqlQuery("SELECT (1 + ?)::INTEGER AS v"); err != nil {
		t.Fatal(err)
	}
	bindInt32(t, stmt, 41)
	if err := stmt.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare after Bind: %v", err)
	}
	reader, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	if got := readInt32s(t, reader); len(got) != 1 || got[0] != 42 {
		t.Errorf("rows = %v, want [42]", got)
	}
	if counter.prepares != 1 {
		t.Errorf("inner Prepare calls = %d, want 1", counter.prepares)
	}
}

// GetInfo reports this driver, not the upstream Flight SQL driver, while
// the server's vendor fields pass through.
func TestIntegrationGetInfoDriverIdentity(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)

	reader, err := cnxn.GetInfo(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	info := readInfo(t, reader)
	if got := info[adbc.InfoDriverName]; got != DriverName {
		t.Errorf("driver name = %v, want %q", got, DriverName)
	}
	if got := info[adbc.InfoDriverVersion]; got != driverVersion() {
		t.Errorf("driver version = %v, want %q", got, driverVersion())
	}
	if got := info[adbc.InfoVendorName]; got != "gizmosql" {
		t.Errorf("vendor name = %v, want gizmosql", got)
	}
}

// readInfo flattens a GetInfo result into code -> Go value.
func readInfo(t *testing.T, reader array.RecordReader) map[adbc.InfoCode]any {
	t.Helper()
	defer reader.Release()
	out := map[adbc.InfoCode]any{}
	for reader.Next() {
		rec := reader.RecordBatch()
		codes := rec.Column(0).(*array.Uint32)
		values := rec.Column(1).(*array.DenseUnion)
		for i := 0; i < int(rec.NumRows()); i++ {
			child := values.Field(values.ChildID(i))
			idx := int(values.ValueOffset(i))
			if child.IsNull(idx) {
				out[adbc.InfoCode(codes.Value(i))] = nil
				continue
			}
			out[adbc.InfoCode(codes.Value(i))] = child.GetOneForMarshal(idx)
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
