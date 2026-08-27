// Server-side query cancellation.
//
// The upstream Flight SQL driver's notion of "cancel" is purely local: it
// cancels the Go context behind the in-flight RPC, which tears down the
// gRPC stream but tells the server nothing. GizmoSQL only interrupts a
// running DuckDB statement when a CancelFlightInfo (or the deprecated
// CancelQuery) action arrives for the session, so a client that gives up
// on a long query — cursor.close(), Ctrl+C, interpreter shutdown — used
// to leave the query running to completion (or until the server's own
// query timeout). The gizmosql-jdbc driver already sends CancelFlightInfo
// in that situation; this file gives the ADBC driver the same behaviour.
//
// Upstream keeps its *flightsql.Client private, so rather than dialling a
// second connection (which would need every TLS/mTLS/dial option
// re-derived), the wrapper injects gRPC interceptors as dial options.
// During database.Open they observe the *grpc.ClientConn upstream
// dialled, and CancelFlightInfo is later issued over that same
// multiplexed connection. Flight's client middleware — including
// upstream's bearer-auth middleware, which holds the session token after
// the Handshake — is installed on that ClientConn as gRPC interceptors,
// so the cancel RPC is authenticated as the same session automatically;
// the wrapper must not add an authorization header of its own. GizmoSQL
// keys sessions off the JWT's session_id claim and its cancel handler
// ignores the FlightInfo payload, simply interrupting the session's
// active statement, so any well-formed FlightInfo suffices (upstream does
// not retain the real FlightInfo for the plain ExecuteQuery path anyway).
// Arrow's server-side deserialization rejects an UNKNOWN descriptor type,
// so the request carries a CMD descriptor wrapping a CommandStatementQuery
// for the statement's SQL.
package gizmosql

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	pb "github.com/apache/arrow-go/v18/arrow/flight/gen/flight"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// QueryCanceler is implemented by GizmoSQL connections and statements.
// CancelQuery asks the server to interrupt the session's currently
// executing statement via the CancelFlightInfo action. It is safe to call
// from another goroutine while ExecuteQuery/ExecuteUpdate is blocked; the
// blocked call then fails with the server's interrupt error.
type QueryCanceler interface {
	CancelQuery(ctx context.Context) error
}

// implicitCancelTimeout bounds the best-effort CancelFlightInfo issued
// from Close/Release paths, where the caller cannot supply a context.
const implicitCancelTimeout = 5 * time.Second

// sessionCanceler holds what is needed to cancel a connection's active
// statement: the gRPC connection upstream dialled for the session.
type sessionCanceler struct {
	mu sync.Mutex
	cc grpc.ClientConnInterface
}

// observeConn records the gRPC connection upstream is using.
func (sc *sessionCanceler) observeConn(cc grpc.ClientConnInterface) {
	if cc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.cc = cc
}

// cancelRequest builds the CancelFlightInfo payload: a FlightInfo whose
// descriptor identifies query (possibly empty) as a statement query.
func cancelRequest(query string) (*flight.CancelFlightInfoRequest, error) {
	cmd, err := anypb.New(&pb.CommandStatementQuery{Query: query})
	if err != nil {
		return nil, err
	}
	raw, err := proto.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	return &flight.CancelFlightInfoRequest{Info: &flight.FlightInfo{
		FlightDescriptor: &flight.FlightDescriptor{Type: flight.DescriptorCMD, Cmd: raw},
		TotalRecords:     -1,
		TotalBytes:       -1,
	}}, nil
}

// cancel issues CancelFlightInfo for the session's active statement;
// query is the SQL being cancelled, if known.
func (sc *sessionCanceler) cancel(ctx context.Context, query string) error {
	sc.mu.Lock()
	cc := sc.cc
	sc.mu.Unlock()
	if cc == nil {
		return adbc.Error{
			Code: adbc.StatusInvalidState,
			Msg:  "[GizmoSQL] cannot cancel query: connection has not been established",
		}
	}
	req, err := cancelRequest(query)
	if err != nil {
		return adbc.Error{Code: adbc.StatusInternal, Msg: "[GizmoSQL] building CancelFlightInfo request: " + err.Error()}
	}
	// NewClientFromConn does not own cc; Close is deliberately not called.
	cl := flight.NewClientFromConn(cc, nil)
	res, err := cl.CancelFlightInfo(ctx, req)
	if err != nil {
		return adbc.Error{
			Code: adbc.StatusIO,
			Msg:  "[GizmoSQL] CancelFlightInfo failed: " + err.Error(),
		}
	}
	switch res.GetStatus() {
	case flight.CancelStatusCancelled, flight.CancelStatusCancelling:
		return nil
	case flight.CancelStatusNotCancellable:
		return adbc.Error{Code: adbc.StatusInvalidState, Msg: "[GizmoSQL] query is not cancellable"}
	default:
		return adbc.Error{Code: adbc.StatusUnknown, Msg: "[GizmoSQL] CancelFlightInfo returned an unspecified status"}
	}
}

// sessionCancelerKey carries the sessionCanceler through the context of
// database.Open so the interceptors can populate it from the RPCs upstream
// makes while opening the connection.
type sessionCancelerKey struct{}

func withSessionCanceler(ctx context.Context, sc *sessionCanceler) context.Context {
	return context.WithValue(ctx, sessionCancelerKey{}, sc)
}

func sessionCancelerFrom(ctx context.Context) *sessionCanceler {
	sc, _ := ctx.Value(sessionCancelerKey{}).(*sessionCanceler)
	return sc
}

// captureDialOptions returns the gRPC dial options that let the wrapper
// observe the connection upstream dials for a session. Only RPCs whose
// context carries a sessionCanceler (those made during Open) are
// observed; everything else passes through untouched.
func captureDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			if sc := sessionCancelerFrom(ctx); sc != nil {
				sc.observeConn(cc)
			}
			return invoker(ctx, method, req, reply, cc, opts...)
		}),
		grpc.WithChainStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			if sc := sessionCancelerFrom(ctx); sc != nil {
				sc.observeConn(cc)
			}
			return streamer(ctx, desc, cc, method, opts...)
		}),
	}
}

// execState tracks one streaming execution so the server is asked to
// interrupt it exactly once, and only if the caller abandoned it before
// draining the result.
type execState struct {
	exhausted atomic.Bool // reader.Next returned false (or errored)
	cancelled atomic.Bool // a cancel was already sent for this execution
}

// abandoned reports whether the execution still needs a server-side
// cancel, claiming it so concurrent callers do not double-send.
func (e *execState) abandoned() bool {
	return !e.exhausted.Load() && e.cancelled.CompareAndSwap(false, true)
}

// cancelReader wraps a streaming result so that releasing it before it
// is exhausted (cursor.close(), del cursor, ...) interrupts the query on
// the server. Retain/Release are reference counted because the C shim
// exports the reader (which retains it) and then drops its own reference.
type cancelReader struct {
	array.RecordReader
	state    *execState
	refs     atomic.Int64
	onCancel func()
}

func newCancelReader(inner array.RecordReader, state *execState, onCancel func()) *cancelReader {
	r := &cancelReader{RecordReader: inner, state: state, onCancel: onCancel}
	r.refs.Store(1)
	return r
}

func (r *cancelReader) Retain() {
	r.refs.Add(1)
	r.RecordReader.Retain()
}

func (r *cancelReader) Release() {
	if r.refs.Add(-1) == 0 && r.state.abandoned() {
		r.onCancel()
	}
	r.RecordReader.Release()
}

func (r *cancelReader) Next() bool {
	ok := r.RecordReader.Next()
	if !ok {
		r.state.exhausted.Store(true)
	}
	return ok
}

// Schema/Record/RecordBatch/Err are promoted from the embedded reader.
var _ array.RecordReader = (*cancelReader)(nil)

var errNoCanceler = errors.New("[GizmoSQL] statement has no session canceler")
