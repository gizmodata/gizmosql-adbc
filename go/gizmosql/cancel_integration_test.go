package gizmosql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// longQuery is CPU-bound for far longer than any test timeout, so the only
// way it returns promptly is a server-side interrupt.
const longQuery = "SELECT sum(a.range * b.range) FROM range(100000000) a, range(100000) b"

// Markers in gizmosql_server's --print-queries log: a statement starting
// to run, and DoCancelActiveStatement interrupting the active statement.
const (
	serverAttemptMarker  = "status=attempt"
	serverCanceledMarker = "successfully canceled"
)

func openIntegration(t *testing.T) (adbc.Connection, *serverLog) {
	t.Helper()
	port, logs := startServerCapturing(t)
	db, err := NewDriver(memory.DefaultAllocator).NewDatabase(connectOptions(port))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cnxn, err := db.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cnxn.Close() })
	return cnxn, logs
}

func waitForLog(t *testing.T, logs *serverLog, marker string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), marker) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server log did not contain %q within %v", marker, timeout)
}

// assertSessionUsable proves the session survived the cancel: the same
// connection must still answer a trivial query.
func assertSessionUsable(t *testing.T, cnxn adbc.Connection) {
	t.Helper()
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery("SELECT 42"); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("query after cancel failed: %v", err)
	}
	defer rdr.Release()
	if !rdr.Next() || rdr.RecordBatch().NumRows() != 1 {
		t.Fatalf("query after cancel returned no rows (err=%v)", rdr.Err())
	}
}

// Explicit cancel from another goroutine while ExecuteQuery is blocked —
// what the Python driver manager does on Ctrl+C and what
// cursor.adbc_cancel() does.
func TestIntegrationCancelQueryInterruptsRunningStatement(t *testing.T) {
	cnxn, logs := openIntegration(t)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(longQuery); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		rdr, _, err := stmt.ExecuteQuery(context.Background())
		if err == nil {
			for rdr.Next() {
			}
			err = rdr.Err()
			rdr.Release()
		}
		done <- err
	}()

	// Give the server time to start executing before cancelling.
	waitForLog(t, logs, serverAttemptMarker, 20*time.Second)
	time.Sleep(time.Second)
	if err := stmt.(QueryCanceler).CancelQuery(context.Background()); err != nil {
		t.Fatalf("CancelQuery: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("long query completed in %v; expected it to be interrupted", time.Since(start))
		}
		t.Logf("interrupted after %v: %v", time.Since(start), err)
	case <-time.After(30 * time.Second):
		t.Fatal("query was not interrupted within 30s of CancelQuery")
	}
	waitForLog(t, logs, serverCanceledMarker, 5*time.Second)
	assertSessionUsable(t, cnxn)
}

// Connection-level cancel (AdbcConnectionCancel) reaches the same server
// endpoint and interrupts whatever the session is running.
func TestIntegrationConnectionCancelQuery(t *testing.T) {
	cnxn, logs := openIntegration(t)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(longQuery); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := stmt.ExecuteUpdate(context.Background())
		done <- err
	}()
	waitForLog(t, logs, serverAttemptMarker, 20*time.Second)
	time.Sleep(time.Second)
	if err := cnxn.(QueryCanceler).CancelQuery(context.Background()); err != nil {
		t.Fatalf("CancelQuery: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the update to be interrupted")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("update was not interrupted within 30s")
	}
	waitForLog(t, logs, serverCanceledMarker, 5*time.Second)
	assertSessionUsable(t, cnxn)
}

// Closing the statement while its query is still executing on the
// server — the path cursor.close() / statement release take — must
// interrupt it rather than let it run to completion.
func TestIntegrationCloseWhileExecutingCancelsOnServer(t *testing.T) {
	cnxn, logs := openIntegration(t)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery(longQuery); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	waitForLog(t, logs, serverAttemptMarker, 20*time.Second)
	time.Sleep(time.Second)

	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	waitForLog(t, logs, serverCanceledMarker, 5*time.Second)

	// The reader observes the interrupt, and releasing it afterwards must
	// not send a second cancel.
	for rdr.Next() {
	}
	if rdr.Err() == nil {
		t.Fatal("expected the abandoned query to fail with the server's interrupt")
	}
	rdr.Release()
	if n := strings.Count(logs.String(), serverCanceledMarker); n != 1 {
		t.Fatalf("server logged %d cancels, want exactly 1", n)
	}
	assertSessionUsable(t, cnxn)
}

// GizmoSQL returns the schema in the FlightInfo, so ExecuteQuery returns
// before the query has run; execution happens on the background DoGet.
// Releasing that result while the server is still executing — what
// cursor.close() / interpreter shutdown do — must interrupt the query.
func TestIntegrationAbandonedResultCancelsRunningQuery(t *testing.T) {
	cnxn, logs := openIntegration(t)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery(longQuery); err != nil {
		t.Fatal(err)
	}
	type result struct {
		rdr array.RecordReader
		err error
	}
	got := make(chan result, 1)
	go func() {
		rdr, _, err := stmt.ExecuteQuery(context.Background())
		got <- result{rdr, err}
	}()
	var res result
	select {
	case res = <-got:
	case <-time.After(20 * time.Second):
		t.Fatal("ExecuteQuery blocked; expected it to return before the query finished")
	}
	if res.err != nil {
		t.Fatalf("ExecuteQuery: %v", res.err)
	}
	waitForLog(t, logs, serverAttemptMarker, 20*time.Second)
	time.Sleep(time.Second)

	res.rdr.Release() // walk away without reading
	waitForLog(t, logs, serverCanceledMarker, 5*time.Second)
	assertSessionUsable(t, cnxn)
}
