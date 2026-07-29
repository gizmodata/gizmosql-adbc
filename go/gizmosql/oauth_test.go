// Licensed under the Apache License, Version 2.0.

// Ported from the 1.x Python driver's tests/test_oauth_unit.py — mock
// OAuth HTTP servers, no real GizmoSQL server or browser involved.

package gizmosql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newOAuthServer serves /oauth/initiate and /oauth/token/{uuid}. The
// tokenResponses are served in order; the last one repeats.
func newOAuthServer(t *testing.T, tokenResponses ...map[string]any) *httptest.Server {
	t.Helper()
	var polls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/initiate", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"session_uuid": "test-uuid-1234",
			"auth_url":     "https://idp.example.com/authorize?state=abc",
		})
	})
	mux.HandleFunc("/oauth/token/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "test-uuid-1234") {
			json.NewEncoder(w).Encode(map[string]any{"status": "not_found"})
			return
		}
		i := int(polls.Add(1)) - 1
		if i >= len(tokenResponses) {
			i = len(tokenResponses) - 1
		}
		json.NewEncoder(w).Encode(tokenResponses[i])
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testCfg(baseURL string) OAuthConfig {
	return OAuthConfig{
		BaseURL:        baseURL,
		PollInterval:   10 * time.Millisecond,
		Timeout:        5 * time.Second,
		AuthURLHandler: func(string) {}, // never touch a real browser in tests
	}
}

func TestOAuthSuccessAfterPending(t *testing.T) {
	srv := newOAuthServer(t,
		map[string]any{"status": "pending"},
		map[string]any{"status": "pending"},
		map[string]any{"status": "complete", "token": "jwt-token-xyz"},
	)
	var gotAuthURL string
	cfg := testCfg(srv.URL)
	cfg.AuthURLHandler = func(u string) { gotAuthURL = u }

	result, err := GetOAuthToken(context.Background(), cfg)
	if err != nil {
		t.Fatalf("GetOAuthToken: %v", err)
	}
	if result.Token != "jwt-token-xyz" {
		t.Errorf("Token = %q, want jwt-token-xyz", result.Token)
	}
	if result.SessionUUID != "test-uuid-1234" {
		t.Errorf("SessionUUID = %q", result.SessionUUID)
	}
	if gotAuthURL != "https://idp.example.com/authorize?state=abc" {
		t.Errorf("auth URL not delivered to handler: %q", gotAuthURL)
	}
}

func TestOAuthErrorStatus(t *testing.T) {
	srv := newOAuthServer(t, map[string]any{"status": "error", "error": "idp exploded"})
	_, err := GetOAuthToken(context.Background(), testCfg(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "idp exploded") {
		t.Fatalf("want error containing 'idp exploded', got %v", err)
	}
}

func TestOAuthNotFound(t *testing.T) {
	srv := newOAuthServer(t, map[string]any{"status": "not_found"})
	_, err := GetOAuthToken(context.Background(), testCfg(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
}

func TestOAuthUnexpectedStatus(t *testing.T) {
	srv := newOAuthServer(t, map[string]any{"status": "wat"})
	_, err := GetOAuthToken(context.Background(), testCfg(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "unexpected token poll status") {
		t.Fatalf("want unexpected-status error, got %v", err)
	}
}

func TestOAuthCompleteWithoutToken(t *testing.T) {
	srv := newOAuthServer(t, map[string]any{"status": "complete"})
	_, err := GetOAuthToken(context.Background(), testCfg(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "no token") {
		t.Fatalf("want missing-token error, got %v", err)
	}
}

func TestOAuthTimeout(t *testing.T) {
	srv := newOAuthServer(t, map[string]any{"status": "pending"})
	cfg := testCfg(srv.URL)
	cfg.Timeout = 50 * time.Millisecond
	_, err := GetOAuthToken(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestOAuthMalformedInitiate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/initiate", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"nope": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_, err := GetOAuthToken(context.Background(), testCfg(srv.URL))
	if err == nil || !strings.Contains(err.Error(), "unexpected response from /oauth/initiate") {
		t.Fatalf("want initiate-shape error, got %v", err)
	}
}

func TestOAuthDiscoveryFallsBackToHTTP(t *testing.T) {
	// A plain-HTTP OAuth server: the HTTPS probe fails, discovery falls
	// back to HTTP — parity with 1.x _discover_oauth_base_url.
	srv := newOAuthServer(t, map[string]any{"status": "complete", "token": "tok"})
	addr := srv.Listener.Addr().String()
	host, portStr, _ := strings.Cut(addr, ":")
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := OAuthConfig{
		Host:           host,
		Port:           port,
		PollInterval:   10 * time.Millisecond,
		Timeout:        5 * time.Second,
		AuthURLHandler: func(string) {},
	}
	result, err := GetOAuthToken(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discovery flow failed: %v", err)
	}
	if result.Token != "tok" {
		t.Errorf("Token = %q, want tok", result.Token)
	}
}

func TestDriverExternalAuthInjectsToken(t *testing.T) {
	srv := newOAuthServer(t, map[string]any{"status": "complete", "token": "jwt-abc"})
	rec := &recordingDriver{}
	drv := &driverImpl{inner: rec}

	db, err := drv.NewDatabase(map[string]string{
		"uri":                     "gizmosql://localhost:31337",
		OptionAuthType:            "external",
		OptionOAuthURL:            srv.URL,
		OptionOAuthOpenBrowser:    "false",
		OptionOAuthTimeoutSeconds: "5",
	})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.Close()

	if got := rec.gotOpts["username"]; got != "token" {
		t.Errorf("username = %q, want token", got)
	}
	if got := rec.gotOpts["password"]; got != "jwt-abc" {
		t.Errorf("password = %q, want jwt-abc", got)
	}
	for k := range rec.gotOpts {
		if strings.HasPrefix(k, "adbc.gizmosql.") {
			t.Errorf("gizmosql option %q leaked to the upstream driver", k)
		}
	}
	if got := rec.gotOpts["uri"]; got != "flightsql://localhost:31337" {
		t.Errorf("uri = %q, want rewritten flightsql:// form", got)
	}
}

func TestDriverExternalAuthRequiresHostOrURL(t *testing.T) {
	rec := &recordingDriver{}
	drv := &driverImpl{inner: rec}
	_, err := drv.NewDatabase(map[string]string{OptionAuthType: "external"})
	if err == nil || !strings.Contains(err.Error(), "requires either") {
		t.Fatalf("want missing-host error, got %v", err)
	}
}

func TestDriverInvalidAuthType(t *testing.T) {
	rec := &recordingDriver{}
	drv := &driverImpl{inner: rec}
	_, err := drv.NewDatabase(map[string]string{
		"uri":          "gizmosql://localhost:31337",
		OptionAuthType: "bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("want invalid auth_type error, got %v", err)
	}
}

func TestDriverPasswordAuthPassesThrough(t *testing.T) {
	rec := &recordingDriver{}
	drv := &driverImpl{inner: rec}
	db, err := drv.NewDatabase(map[string]string{
		"uri":          "gizmosql://localhost:31337",
		"username":     "u",
		"password":     "p",
		OptionAuthType: "password",
	})
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.Close()
	if rec.gotOpts["username"] != "u" || rec.gotOpts["password"] != "p" {
		t.Errorf("credentials altered: %v", rec.gotOpts)
	}
	if _, ok := rec.gotOpts[OptionAuthType]; ok {
		t.Error("auth_type option leaked to the upstream driver")
	}
}

func TestExtractHost(t *testing.T) {
	cases := map[string]string{
		"grpc+tls://localhost:31337":            "localhost",
		"grpc://myhost:31337":                   "myhost",
		"grpc+tls://gizmosql.example.com:31337": "gizmosql.example.com",
		"grpc+tls://localhost":                  "localhost",
		"grpc+tcp://192.168.1.1:31337":          "192.168.1.1",
		"gizmosql://gizmosql.example.com:31337": "gizmosql.example.com",
		"gizmosql://myhost:31337?transport=tcp": "myhost",
		"gizmosql://myhost?transport=tcp":       "myhost",
		"":                                      "",
	}
	for uri, want := range cases {
		if got := extractHost(uri); got != want {
			t.Errorf("extractHost(%q) = %q, want %q", uri, got, want)
		}
	}
}
