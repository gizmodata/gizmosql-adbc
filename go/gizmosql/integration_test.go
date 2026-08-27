// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const (
	testUsername = "gizmosql_user"
	testPassword = "gizmosql_password"
)

// findServerBinary locates a gizmosql_server executable: the
// GIZMOSQL_SERVER_BIN env var, then $PATH, then the newest version in the
// gizmosql pip package's binary cache (~/.cache/gizmosql/<ver>/stable/).
func findServerBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("GIZMOSQL_SERVER_BIN"); bin != "" {
		return bin
	}
	if bin, err := exec.LookPath("gizmosql_server"); err == nil {
		return bin
	}
	cacheRoot := os.Getenv("GIZMOSQL_CACHE_DIR")
	if cacheRoot == "" {
		if runtime.GOOS == "windows" {
			if base := os.Getenv("LOCALAPPDATA"); base != "" {
				cacheRoot = filepath.Join(base, "gizmosql", "Cache")
			}
		} else {
			base := os.Getenv("XDG_CACHE_HOME")
			if base == "" {
				home, err := os.UserHomeDir()
				if err == nil {
					base = filepath.Join(home, ".cache")
				}
			}
			cacheRoot = filepath.Join(base, "gizmosql")
		}
	}
	matches, _ := filepath.Glob(filepath.Join(cacheRoot, "*", "stable", "gizmosql_server"))
	if len(matches) == 0 {
		t.Skip("no gizmosql_server binary found (set GIZMOSQL_SERVER_BIN, add to PATH, " +
			"or `pip install gizmosql` and run its Server once to populate the cache)")
	}
	sort.Strings(matches) // version dirs sort lexically; newest last is good enough
	return matches[len(matches)-1]
}

// writeSelfSignedCert mints an ECDSA P-256 certificate for
// localhost/127.0.0.1 and writes cert + key PEMs into dir.
func writeSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startServer launches a TLS-enabled GizmoSQL server subprocess and
// returns its Flight SQL port. Cleanup is registered on t.
func startServer(t *testing.T) int {
	port, _ := startServerCapturing(t)
	return port
}

// serverLog is a goroutine-safe buffer for the server subprocess's
// combined stdout/stderr, so tests can assert on server-side events
// (e.g. that a statement was actually interrupted) while the output
// still reaches the test's stderr.
type serverLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *serverLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *serverLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// startServerCapturing is startServer plus a handle on the server's log.
func startServerCapturing(t *testing.T) (int, *serverLog) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	bin := findServerBinary(t)
	dir := t.TempDir()
	certPath, keyPath := writeSelfSignedCert(t, dir)
	port, healthPort := freePort(t), freePort(t)

	cmd := exec.Command(bin,
		"--hostname", "127.0.0.1",
		"--port", fmt.Sprint(port),
		"--health-port", fmt.Sprint(healthPort),
		"--username", testUsername,
		"--tls", certPath, keyPath,
		// Log statement lifecycle events so tests can assert on them.
		"--print-queries",
	)
	cmd.Env = append(os.Environ(), "GIZMOSQL_PASSWORD="+testPassword)
	logs := &serverLog{}
	cmd.Stdout, cmd.Stderr = io.MultiWriter(os.Stderr, logs), io.MultiWriter(os.Stderr, logs)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gizmosql_server (%s): %v", bin, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(30 * time.Second)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return port, logs
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("gizmosql_server did not accept connections on %s within 30s", addr)
	return 0, nil
}

func connectOptions(port int) map[string]string {
	return map[string]string{
		"uri":      fmt.Sprintf("gizmosql://127.0.0.1:%d", port),
		"username": testUsername,
		"password": testPassword,
		"adbc.flight.sql.client_option.tls_skip_verify": "true",
	}
}

func TestIntegrationSelectOne(t *testing.T) {
	port := startServer(t)
	ctx := context.Background()

	drv := NewDriver(memory.DefaultAllocator)
	db, err := drv.NewDatabase(connectOptions(port))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.Close()

	cnxn, err := db.Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cnxn.Close()

	stmt, err := cnxn.NewStatement()
	if err != nil {
		t.Fatalf("NewStatement: %v", err)
	}
	defer stmt.Close()

	if err := stmt.SetSqlQuery("SELECT 1 AS value"); err != nil {
		t.Fatalf("SetSqlQuery: %v", err)
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		t.Fatalf("ExecuteQuery: %v", err)
	}
	defer reader.Release()

	if !reader.Next() {
		t.Fatal("expected at least one record batch")
	}
	rec := reader.Record()
	if rec.NumCols() != 1 || rec.NumRows() != 1 {
		t.Fatalf("expected 1x1 result, got %dx%d", rec.NumRows(), rec.NumCols())
	}
	var got int64
	switch col := rec.Column(0).(type) {
	case *array.Int32:
		got = int64(col.Value(0))
	case *array.Int64:
		got = col.Value(0)
	default:
		t.Fatalf("unexpected column type %T", rec.Column(0))
	}
	if got != 1 {
		t.Errorf("SELECT 1 = %d, want 1", got)
	}
}

func TestIntegrationLegacySchemeParity(t *testing.T) {
	port := startServer(t)
	ctx := context.Background()

	opts := connectOptions(port)
	opts["uri"] = fmt.Sprintf("grpc+tls://127.0.0.1:%d", port)

	drv := NewDriver(memory.DefaultAllocator)
	db, err := drv.NewDatabase(opts)
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.Close()

	cnxn, err := db.Open(ctx)
	if err != nil {
		t.Fatalf("Open over grpc+tls://: %v", err)
	}
	cnxn.Close()
}
