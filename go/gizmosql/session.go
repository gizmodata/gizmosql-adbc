// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow/flight"
)

// sessionOpenTimeout bounds the session-creating call made at connect.
const sessionOpenTimeout = 30 * time.Second

// openServerSession makes sure the server has a session for this
// connection as soon as it is opened.
//
// GizmoSQL creates a client session lazily, on the first call that uses
// one, and its GetSessionOptions handler deliberately never creates one:
// it doubles as a liveness probe, so it answers "Session not found" for a
// session that does not exist (yet). The upstream Flight SQL driver reads
// the current catalog and schema through GetSessionOptions, so on a fresh
// autocommit connection that had not executed anything,
// conn.adbc_current_catalog / adbc_current_db_schema failed with
// "Unauthenticated: Session not found — it may have been evicted".
//
// An empty SetSessionOptions request uses the server's creating session
// lookup and changes nothing else, which is what the GizmoSQL JDBC driver
// does at connect (Handshake + SetSessionOptions). It is sent over the
// connection upstream dialled, so upstream's bearer-auth middleware
// authenticates it as this session. Best effort: a server without session
// options support still gets a working connection.
func openServerSession(ctx context.Context, sc *sessionCanceler) {
	sc.mu.Lock()
	cc := sc.cc
	sc.mu.Unlock()
	if cc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sessionOpenTimeout)
	defer cancel()
	// NewClientFromConn does not own cc; Close is deliberately not called.
	cl := flight.NewClientFromConn(cc, nil)
	_, _ = cl.SetSessionOptions(ctx, &flight.SetSessionOptionsRequest{})
}
