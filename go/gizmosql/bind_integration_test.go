// Licensed under the Apache License, Version 2.0.

// Live-server tests for implicit Prepare on Bind/BindStream: consumers
// with no prepare step (e.g. the Node.js adbc-driver-manager, whose
// connection.query(sql, params) is setSqlQuery + bind + executeQuery)
// must still be able to bind parameters to a SQL query.

package gizmosql

import (
	"context"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// countingStatement wraps the inner (upstream) statement to count
// Prepare calls that actually reach it.
type countingStatement struct {
	adbc.Statement
	prepares int
}

func (c *countingStatement) Prepare(ctx context.Context) error {
	c.prepares++
	return c.Statement.Prepare(ctx)
}

// newCountingStatement opens a statement on cnxn and splices a
// countingStatement between the wrapper and the upstream statement.
func newCountingStatement(t *testing.T, cnxn adbc.Connection) (adbc.Statement, *countingStatement) {
	t.Helper()
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatalf("NewStatement: %v", err)
	}
	t.Cleanup(func() { stmt.Close() })
	wrapper := stmt.(*statement)
	counter := &countingStatement{Statement: wrapper.Statement}
	wrapper.Statement = counter
	return stmt, counter
}

func int32Batch(t *testing.T, name string, vals ...int32) arrow.RecordBatch {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.PrimitiveTypes.Int32}}, nil)
	bldr := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer bldr.Release()
	bldr.Field(0).(*array.Int32Builder).AppendValues(vals, nil)
	return bldr.NewRecordBatch()
}

// bindInt32 binds a single-column int32 batch and releases the caller's
// reference.
func bindInt32(t *testing.T, stmt adbc.Statement, vals ...int32) {
	t.Helper()
	rec := int32Batch(t, "p", vals...)
	defer rec.Release()
	if err := stmt.Bind(context.Background(), rec); err != nil {
		t.Fatalf("Bind: %v", err)
	}
}

// readInt32s drains a reader of one int32 column into a slice.
func readInt32s(t *testing.T, reader array.RecordReader) []int32 {
	t.Helper()
	defer reader.Release()
	var out []int32
	for reader.Next() {
		col := reader.RecordBatch().Column(0).(*array.Int32)
		for i := 0; i < col.Len(); i++ {
			out = append(out, col.Value(i))
		}
	}
	if err := reader.Err(); err != nil {
		t.Fatalf("reader: %v", err)
	}
	return out
}

func TestIntegrationBindWithoutPrepareQuery(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)
	ctx := context.Background()

	execQuery(t, cnxn, "CREATE TABLE autoprep_q (id INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS autoprep_q")
	execQuery(t, cnxn, "INSERT INTO autoprep_q VALUES (1), (2), (3)")

	stmt, counter := newCountingStatement(t, cnxn)
	if err := stmt.SetSqlQuery("SELECT id FROM autoprep_q WHERE id = ?"); err != nil {
		t.Fatal(err)
	}
	bindInt32(t, stmt, 2) // no explicit Prepare
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("ExecuteQuery after unprepared Bind: %v", err)
	}
	if got := readInt32s(t, reader); len(got) != 1 || got[0] != 2 {
		t.Errorf("rows = %v, want [2]", got)
	}
	if counter.prepares != 1 {
		t.Errorf("inner Prepare calls = %d, want 1 (implicit)", counter.prepares)
	}
}

func TestIntegrationBindWithoutPrepareUpdate(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)
	ctx := context.Background()

	execQuery(t, cnxn, "CREATE TABLE autoprep_u (id INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS autoprep_u")

	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery("INSERT INTO autoprep_u VALUES (?)"); err != nil {
		t.Fatal(err)
	}
	bindInt32(t, stmt, 42) // no explicit Prepare
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatalf("ExecuteUpdate after unprepared Bind: %v", err)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM autoprep_u WHERE id = 42"); got != 1 {
		t.Errorf("COUNT(*) WHERE id = 42 = %d, want 1", got)
	}
}

func TestIntegrationExplicitPrepareThenBindPreparesOnce(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)
	ctx := context.Background()

	execQuery(t, cnxn, "CREATE TABLE autoprep_p (id INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS autoprep_p")
	execQuery(t, cnxn, "INSERT INTO autoprep_p VALUES (41), (42), (43)")

	stmt, counter := newCountingStatement(t, cnxn)
	if err := stmt.SetSqlQuery("SELECT id FROM autoprep_p WHERE id = ?"); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Prepare(ctx); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	bindInt32(t, stmt, 42)
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	if got := readInt32s(t, reader); len(got) != 1 || got[0] != 42 {
		t.Errorf("rows = %v, want [42]", got)
	}
	if counter.prepares != 1 {
		t.Errorf("inner Prepare calls = %d, want exactly 1 (explicit, not repeated by Bind)", counter.prepares)
	}
}

func TestIntegrationBulkIngestUnaffectedByAutoPrepare(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)
	ctx := context.Background()
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS autoprep_ingest")

	stmt, counter := newCountingStatement(t, cnxn)
	if err := stmt.SetOption(adbc.OptionKeyIngestTargetTable, "autoprep_ingest"); err != nil {
		t.Fatalf("set ingest target: %v", err)
	}
	bindInt32(t, stmt, 7, 8, 9)
	if _, err := stmt.ExecuteUpdate(ctx); err != nil {
		t.Fatalf("ingest ExecuteUpdate: %v", err)
	}
	if got := queryInt64(t, cnxn, "SELECT COUNT(*) FROM autoprep_ingest"); got != 3 {
		t.Errorf("ingest COUNT(*) = %d, want 3", got)
	}
	if counter.prepares != 0 {
		t.Errorf("inner Prepare calls = %d, want 0 for bulk ingest", counter.prepares)
	}
}

func TestIntegrationRebindAfterNewQueryReprepares(t *testing.T) {
	port := startServer(t)
	cnxn := openConn(t, port)
	ctx := context.Background()

	execQuery(t, cnxn, "CREATE TABLE autoprep_r (id INT, tenfold INT)")
	defer execQuery(t, cnxn, "DROP TABLE IF EXISTS autoprep_r")
	execQuery(t, cnxn, "INSERT INTO autoprep_r VALUES (1, 10), (5, 50)")

	stmt, counter := newCountingStatement(t, cnxn)

	// First query, bound without Prepare.
	if err := stmt.SetSqlQuery("SELECT id FROM autoprep_r WHERE id = ?"); err != nil {
		t.Fatal(err)
	}
	bindInt32(t, stmt, 1)
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("first ExecuteQuery: %v", err)
	}
	if got := readInt32s(t, reader); len(got) != 1 || got[0] != 1 {
		t.Errorf("first rows = %v, want [1]", got)
	}
	if counter.prepares != 1 {
		t.Fatalf("inner Prepare calls after first bind = %d, want 1", counter.prepares)
	}

	// New query on the same statement (the wrapper recreates the inner
	// statement because it was bound), bound again without Prepare —
	// must prepare against the NEW query, not reuse the old one.
	if err := stmt.SetSqlQuery("SELECT tenfold FROM autoprep_r WHERE id = ?"); err != nil {
		t.Fatal(err)
	}
	// resetForNewQuery swapped in a fresh inner statement; re-splice the
	// counter so the second Prepare is observed too.
	wrapper := stmt.(*statement)
	counter2 := &countingStatement{Statement: wrapper.Statement}
	wrapper.Statement = counter2
	bindInt32(t, stmt, 5)
	reader, _, err = stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("second ExecuteQuery: %v", err)
	}
	if got := readInt32s(t, reader); len(got) != 1 || got[0] != 50 {
		t.Errorf("second rows = %v, want [50] (re-prepared against new query?)", got)
	}
	if counter2.prepares != 1 {
		t.Errorf("inner Prepare calls after re-bind = %d, want 1 (re-prepare against new query)", counter2.prepares)
	}
}
