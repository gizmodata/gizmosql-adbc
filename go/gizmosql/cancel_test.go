package gizmosql

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// ---------------------------------------------------------------------
// Pure unit tests: reader/statement bookkeeping, no network.
// ---------------------------------------------------------------------

// stubReader yields n single-row batches then ends.
type stubReader struct {
	schema   *arrow.Schema
	n, i     int
	rec      arrow.RecordBatch
	released int
}

func newStubReader(n int) *stubReader {
	schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(1)
	return &stubReader{schema: schema, n: n, rec: b.NewRecordBatch()}
}

func (r *stubReader) Retain()                        {}
func (r *stubReader) Release()                       { r.released++ }
func (r *stubReader) Schema() *arrow.Schema          { return r.schema }
func (r *stubReader) Next() bool                     { r.i++; return r.i <= r.n }
func (r *stubReader) Record() arrow.RecordBatch      { return r.rec }
func (r *stubReader) RecordBatch() arrow.RecordBatch { return r.rec }
func (r *stubReader) Err() error                     { return nil }

func TestCancelReaderReleaseBeforeExhaustionCancelsOnce(t *testing.T) {
	calls := 0
	rdr := newCancelReader(newStubReader(3), &execState{}, func() { calls++ })

	// Mirror the C shim: export retains, shim drops its own ref, consumer
	// reads one batch, then releases the exported stream.
	rdr.Retain()
	rdr.Release()
	if calls != 0 {
		t.Fatalf("cancel fired while a reference was still held")
	}
	if !rdr.Next() {
		t.Fatal("expected a batch")
	}
	rdr.Release()
	if calls != 1 {
		t.Fatalf("cancel calls = %d, want 1", calls)
	}
}

func TestCancelReaderExhaustedNeverCancels(t *testing.T) {
	calls := 0
	rdr := newCancelReader(newStubReader(2), &execState{}, func() { calls++ })
	for rdr.Next() {
	}
	rdr.Release()
	if calls != 0 {
		t.Fatalf("cancel fired for a fully drained reader (%d calls)", calls)
	}
}

func TestStatementCloseCancelsAbandonedOnlyOnce(t *testing.T) {
	calls := 0
	st := &statement{}
	state := &execState{}
	st.current = state
	rdr := newCancelReader(newStubReader(3), state, func() { calls++ })
	_ = rdr.Next()

	// Close claims the cancel; the later reader release must not re-send.
	if !state.abandoned() {
		t.Fatal("first claim should succeed")
	}
	calls++ // stands in for the RPC the statement would issue
	rdr.Release()
	if calls != 1 {
		t.Fatalf("cancel calls = %d, want exactly 1", calls)
	}
	if state.abandoned() {
		t.Fatal("abandoned() must be one-shot")
	}
}

func TestCancelRequestCarriesStatementQueryDescriptor(t *testing.T) {
	req, err := cancelRequest("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	desc := req.GetInfo().GetFlightDescriptor()
	if desc.GetType() != flight.DescriptorCMD {
		t.Fatalf("descriptor type = %v, want CMD", desc.GetType())
	}
	var cmd anypb.Any
	if err := proto.Unmarshal(desc.GetCmd(), &cmd); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cmd.TypeUrl, "CommandStatementQuery") {
		t.Fatalf("cmd type = %s, want CommandStatementQuery", cmd.TypeUrl)
	}
}

func TestSessionCancelerBeforeConnect(t *testing.T) {
	err := (&sessionCanceler{}).cancel(context.Background(), "")
	var ae adbc.Error
	if !errors.As(err, &ae) || ae.Code != adbc.StatusInvalidState {
		t.Fatalf("expected StatusInvalidState, got %v", err)
	}
}

func TestStatementCancelQueryWithoutCanceler(t *testing.T) {
	if err := (&statement{}).CancelQuery(context.Background()); !errors.Is(err, errNoCanceler) {
		t.Fatalf("expected errNoCanceler, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Fake Flight server: exercises the interceptor capture and the
// CancelFlightInfo round trip end to end through the real upstream driver,
// without needing a GizmoSQL binary.
// ---------------------------------------------------------------------

type fakeFlightServer struct {
	flight.BaseFlightServer
	schema *arrow.Schema

	mu          sync.Mutex
	cancelAuths []string // authorization header of each CancelFlightInfo seen
	batches     int      // batches DoGet emits per ticket
}

func (f *fakeFlightServer) cancels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelAuths...)
}

func (f *fakeFlightServer) GetFlightInfo(ctx context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	var cmd anypb.Any
	if err := proto.Unmarshal(desc.Cmd, &cmd); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// Upstream probes GetSqlInfo during Open and tolerates failure; only
	// serve statement queries.
	if !strings.HasSuffix(cmd.TypeUrl, "CommandStatementQuery") {
		return nil, status.Error(codes.Unimplemented, cmd.TypeUrl)
	}
	return &flight.FlightInfo{
		Schema:           flight.SerializeSchema(f.schema, memory.DefaultAllocator),
		FlightDescriptor: desc,
		Endpoint:         []*flight.FlightEndpoint{{Ticket: &flight.Ticket{Ticket: []byte("q")}}},
		TotalRecords:     -1,
		TotalBytes:       -1,
	}, nil
}

func (f *fakeFlightServer) DoGet(_ *flight.Ticket, fs flight.FlightService_DoGetServer) error {
	w := flight.NewRecordWriter(fs, ipc.WithSchema(f.schema))
	defer w.Close()
	b := array.NewRecordBuilder(memory.DefaultAllocator, f.schema)
	defer b.Release()
	for i := 0; i < f.batches; i++ {
		b.Field(0).(*array.Int64Builder).Append(int64(i))
		rec := b.NewRecordBatch()
		err := w.Write(rec)
		rec.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeFlightServer) DoAction(action *flight.Action, fs flight.FlightService_DoActionServer) error {
	if action.Type != flight.CancelFlightInfoActionType {
		return status.Error(codes.Unimplemented, action.Type)
	}
	var req flight.CancelFlightInfoRequest
	if err := proto.Unmarshal(action.Body, &req); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	// Mirror Arrow C++'s FlightInfo deserialization, which gizmosql_server
	// runs before reaching its cancel handler.
	if req.Info.GetFlightDescriptor().GetType() == flight.DescriptorUNKNOWN {
		return status.Error(codes.InvalidArgument, "Client sent UNKNOWN descriptor type")
	}
	md, _ := metadata.FromIncomingContext(fs.Context())
	auth := strings.Join(md.Get("authorization"), ",")
	f.mu.Lock()
	f.cancelAuths = append(f.cancelAuths, auth)
	f.mu.Unlock()
	body, err := proto.Marshal(&flight.CancelFlightInfoResult{Status: flight.CancelStatusCancelled})
	if err != nil {
		return err
	}
	return fs.Send(&flight.Result{Body: body})
}

func startFakeFlightServer(t *testing.T, batches int) (*fakeFlightServer, string) {
	t.Helper()
	fake := &fakeFlightServer{
		schema:  arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil),
		batches: batches,
	}
	srv := flight.NewFlightServer()
	srv.RegisterFlightService(fake)
	if err := srv.Init("127.0.0.1:0"); err != nil {
		t.Fatalf("init fake flight server: %v", err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(srv.Shutdown)
	return fake, "grpc+tcp://" + srv.Addr().String()
}

const fakeToken = "Bearer unit-test-session-token"

func openFake(t *testing.T, batches int) (*fakeFlightServer, adbc.Connection) {
	t.Helper()
	fake, uri := startFakeFlightServer(t, batches)
	db, err := NewDriver(memory.DefaultAllocator).NewDatabase(map[string]string{
		"uri":                                  uri,
		"adbc.flight.sql.authorization_header": fakeToken,
	})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cnxn, err := db.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { cnxn.Close() })
	return fake, cnxn
}

func TestConnectionCancelQuerySendsCancelFlightInfoWithSessionToken(t *testing.T) {
	fake, cnxn := openFake(t, 1)
	qc, ok := cnxn.(QueryCanceler)
	if !ok {
		t.Fatalf("connection %T does not implement QueryCanceler", cnxn)
	}
	if err := qc.CancelQuery(context.Background()); err != nil {
		t.Fatalf("CancelQuery: %v", err)
	}
	got := fake.cancels()
	if len(got) != 1 || got[0] != fakeToken {
		t.Fatalf("server saw cancels %q, want exactly one with %q", got, fakeToken)
	}
}

func TestStatementCancelQueryIsExplicitAndImmediate(t *testing.T) {
	fake, cnxn := openFake(t, 1)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.(QueryCanceler).CancelQuery(context.Background()); err != nil {
		t.Fatalf("CancelQuery: %v", err)
	}
	if n := len(fake.cancels()); n != 1 {
		t.Fatalf("cancels = %d, want 1", n)
	}
}

func TestReleasingUnfinishedResultCancelsOnServer(t *testing.T) {
	fake, cnxn := openFake(t, 3)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if err := stmt.SetSqlQuery("SELECT v FROM t"); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	if !rdr.Next() {
		t.Fatalf("expected a batch, err=%v", rdr.Err())
	}
	rdr.Release() // abandon with batches still pending
	if got := fake.cancels(); len(got) != 1 || got[0] != fakeToken {
		t.Fatalf("server saw cancels %q, want one with the session token", got)
	}
	// Closing the statement afterwards must not send a second cancel.
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.cancels()); n != 1 {
		t.Fatalf("cancels after Close = %d, want still 1", n)
	}
}

func TestDrainedResultDoesNotCancelOnServer(t *testing.T) {
	fake, cnxn := openFake(t, 3)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery("SELECT v FROM t"); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	rows := 0
	for rdr.Next() {
		rows += int(rdr.RecordBatch().NumRows())
	}
	if rdr.Err() != nil || rows != 3 {
		t.Fatalf("rows=%d err=%v", rows, rdr.Err())
	}
	rdr.Release()
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.cancels()); n != 0 {
		t.Fatalf("cancels = %d, want 0 for a fully consumed result", n)
	}
}

func TestStatementCloseCancelsUnreleasedResult(t *testing.T) {
	fake, cnxn := openFake(t, 3)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.SetSqlQuery("SELECT v FROM t"); err != nil {
		t.Fatal(err)
	}
	rdr, _, err := stmt.ExecuteQuery(context.Background())
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	_ = rdr.Next()
	if err := stmt.Close(); err != nil { // close with the reader still live
		t.Fatal(err)
	}
	rdr.Release()
	if n := len(fake.cancels()); n != 1 {
		t.Fatalf("cancels = %d, want exactly 1 (Close, then no repeat on Release)", n)
	}
}

func TestCaptureDialOptionsIgnoreContextsWithoutCanceler(t *testing.T) {
	// The interceptors must be inert for RPCs whose context carries no
	// canceler (everything after Open); they only add cost on Open.
	unary := captureDialOptions()
	if len(unary) != 2 {
		t.Fatalf("expected unary+stream dial options, got %d", len(unary))
	}
	if sessionCancelerFrom(context.Background()) != nil {
		t.Fatal("background context must not yield a canceler")
	}
	sc := &sessionCanceler{}
	if sessionCancelerFrom(withSessionCanceler(context.Background(), sc)) != sc {
		t.Fatal("canceler did not round-trip through the context")
	}
}

func TestSessionCancelerObserveConn(t *testing.T) {
	sc := &sessionCanceler{}
	cc := &grpc.ClientConn{}
	sc.observeConn(cc)
	sc.observeConn(nil)
	if sc.cc != cc {
		t.Fatal("nil connection must not clobber the captured one")
	}
}

// Guard against the implicit-cancel path hanging a Close when the server
// is unreachable: the timeout must bound it.
func TestImplicitCancelIsBounded(t *testing.T) {
	if implicitCancelTimeout > 10*time.Second {
		t.Fatalf("implicitCancelTimeout %v is too long for a Close path", implicitCancelTimeout)
	}
}

// ---------------------------------------------------------------------
// In-flight execute calls (ExecuteUpdate / ExecuteQuery blocked in the
// RPC): Close and CancelQuery must claim the cancel exactly once.
// ---------------------------------------------------------------------

func TestClaimInFlightOnlyWhileCallIsRunning(t *testing.T) {
	st := &statement{}
	if st.claimInFlight() {
		t.Fatal("nothing in flight: claim must fail")
	}
	call := st.beginCall()
	if !st.claimInFlight() {
		t.Fatal("call in flight: first claim must succeed")
	}
	if st.claimInFlight() {
		t.Fatal("claim must be one-shot per call")
	}
	st.endCall(call)
	if st.claimInFlight() {
		t.Fatal("call finished: claim must fail")
	}
}

func TestEndCallIgnoresStaleCalls(t *testing.T) {
	st := &statement{}
	first := st.beginCall()
	second := st.beginCall()
	st.endCall(first) // stale: must not clear the newer call
	if !st.claimInFlight() {
		t.Fatal("newer call still in flight")
	}
	st.endCall(second)
	if st.claimInFlight() {
		t.Fatal("no call in flight after endCall")
	}
}

func TestCancelQueryMarksInFlightCallSoCloseDoesNotResend(t *testing.T) {
	fake, cnxn := openFake(t, 1)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	st := stmt.(*statement)
	call := st.beginCall()
	if err := st.CancelQuery(context.Background()); err != nil {
		t.Fatalf("CancelQuery: %v", err)
	}
	if st.claimInFlight() {
		t.Fatal("explicit CancelQuery must claim the in-flight cancel")
	}
	st.endCall(call)
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.cancels()); n != 1 {
		t.Fatalf("cancels = %d, want exactly 1 (explicit)", n)
	}
}

func TestCloseDuringInFlightCallCancelsOnServer(t *testing.T) {
	fake, cnxn := openFake(t, 1)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	st := stmt.(*statement)
	if err := st.SetSqlQuery("CREATE TABLE t AS SELECT 1"); err != nil {
		t.Fatal(err)
	}
	// Simulate another goroutine parked inside ExecuteUpdate.
	call := st.beginCall()
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	st.endCall(call)
	if n := len(fake.cancels()); n != 1 {
		t.Fatalf("cancels = %d, want exactly 1", n)
	}
}

func TestCloseWithNothingInFlightDoesNotCancel(t *testing.T) {
	fake, cnxn := openFake(t, 1)
	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.cancels()); n != 0 {
		t.Fatalf("cancels = %d, want 0", n)
	}
}
