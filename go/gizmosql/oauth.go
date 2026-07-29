// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OAuth flow for GizmoSQL's server-side code exchange, ported from the
// 1.x Python driver's _oauth.py:
//
//  1. GET /oauth/initiate → {session_uuid, auth_url}
//  2. Open auth_url in the user's browser (or hand it to a callback)
//  3. Poll GET /oauth/token/{uuid} until complete
//  4. Use the identity token for Flight SQL Basic Auth
//     (username "token", password <id_token>)

const (
	DefaultOAuthPort    = 31339
	DefaultPollInterval = time.Second
	DefaultOAuthTimeout = 5 * time.Minute
)

// OAuthResult is the outcome of a successful OAuth flow.
type OAuthResult struct {
	// Token is the identity token (JWT) from the IdP.
	Token string
	// SessionUUID is the session identifier used during the flow.
	SessionUUID string
}

// OAuthError is returned when the OAuth flow fails.
type OAuthError struct {
	msg string
}

func (e *OAuthError) Error() string { return e.msg }

func oauthErrorf(format string, args ...any) error {
	return &OAuthError{msg: fmt.Sprintf(format, args...)}
}

// OAuthConfig configures GetOAuthToken.
type OAuthConfig struct {
	// Host of the GizmoSQL server (used for discovery when BaseURL is
	// empty).
	Host string
	// Port of the OAuth HTTP server (default 31339).
	Port int
	// BaseURL is an explicit OAuth base URL, e.g.
	// "https://gizmosql.example.com:31339". When set, Host/Port are not
	// used and no discovery probing happens.
	BaseURL string
	// TLSSkipVerify skips TLS certificate verification for the OAuth
	// server.
	TLSSkipVerify bool
	// Timeout is the maximum time to wait for the user to complete
	// authentication (default 5 minutes).
	Timeout time.Duration
	// PollInterval is the delay between token polls (default 1s).
	PollInterval time.Duration
	// OpenBrowser opens the auth URL in the local browser when true.
	// When false, the URL is passed to AuthURLHandler (or printed to
	// stderr if no handler is set) — the headless mode.
	OpenBrowser bool
	// AuthURLHandler, when set, receives the authorization URL instead
	// of (or in addition to being told to skip) the local browser. Use
	// this to embed the flow in another UI.
	AuthURLHandler func(authURL string)
}

func (c *OAuthConfig) withDefaults() OAuthConfig {
	out := *c
	if out.Port == 0 {
		out.Port = DefaultOAuthPort
	}
	if out.Timeout == 0 {
		out.Timeout = DefaultOAuthTimeout
	}
	if out.PollInterval == 0 {
		out.PollInterval = DefaultPollInterval
	}
	return out
}

func oauthHTTPClient(tlsSkipVerify bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Transport: transport}
}

func httpGetJSON(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, oauthErrorf("building request for %s: %v", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, oauthErrorf("failed to connect to %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, oauthErrorf("reading response from %s: %v", url, err)
	}
	if resp.StatusCode >= 400 {
		return nil, oauthErrorf("HTTP %d from %s: %s", resp.StatusCode, url, body)
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, oauthErrorf("invalid JSON from %s: %v", url, err)
	}
	return data, nil
}

// discoverOAuthBaseURL probes the server for its OAuth endpoint: HTTPS
// first, then HTTP. The /oauth/initiate response is returned alongside
// the base URL so the round trip is not wasted.
func discoverOAuthBaseURL(
	ctx context.Context, client *http.Client, host string, port int,
) (string, map[string]any, error) {
	httpsBase := fmt.Sprintf("https://%s:%d", host, port)
	data, httpsErr := httpGetJSON(ctx, client, httpsBase+"/oauth/initiate")
	if httpsErr == nil {
		return httpsBase, data, nil
	}
	httpBase := fmt.Sprintf("http://%s:%d", host, port)
	data, httpErr := httpGetJSON(ctx, client, httpBase+"/oauth/initiate")
	if httpErr == nil {
		return httpBase, data, nil
	}
	return "", nil, oauthErrorf(
		"could not connect to OAuth server at %s:%d (tried HTTPS and HTTP): %v",
		host, port, httpErr)
}

// openBrowser opens url in the platform's default browser.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// GetOAuthToken performs the GizmoSQL OAuth browser flow and returns an
// identity token for Flight SQL Basic Auth (username "token", password
// <token>). The context bounds the whole flow in addition to
// cfg.Timeout.
func GetOAuthToken(ctx context.Context, cfg OAuthConfig) (*OAuthResult, error) {
	cfg = cfg.withDefaults()
	client := oauthHTTPClient(cfg.TLSSkipVerify)

	var (
		baseURL string
		data    map[string]any
		err     error
	)
	if cfg.BaseURL != "" {
		baseURL = strings.TrimRight(cfg.BaseURL, "/")
		data, err = httpGetJSON(ctx, client, baseURL+"/oauth/initiate")
	} else {
		if cfg.Host == "" {
			return nil, oauthErrorf("OAuth requires a host or an explicit base URL")
		}
		baseURL, data, err = discoverOAuthBaseURL(ctx, client, cfg.Host, cfg.Port)
	}
	if err != nil {
		return nil, err
	}

	sessionUUID, _ := data["session_uuid"].(string)
	authURL, _ := data["auth_url"].(string)
	if sessionUUID == "" || authURL == "" {
		return nil, oauthErrorf("unexpected response from /oauth/initiate: %v", data)
	}

	switch {
	case cfg.AuthURLHandler != nil:
		cfg.AuthURLHandler(authURL)
	case cfg.OpenBrowser:
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintf(os.Stderr,
				"Could not open a browser (%v). Open this URL to authenticate:\n%s\n",
				err, authURL)
		}
	default:
		fmt.Fprintf(os.Stderr,
			"Open this URL in your browser to authenticate:\n%s\n", authURL)
	}

	pollURL := fmt.Sprintf("%s/oauth/token/%s", baseURL, sessionUUID)
	deadline := time.Now().Add(cfg.Timeout)
	for {
		if time.Now().After(deadline) {
			return nil, oauthErrorf(
				"OAuth flow timed out after %s: the user did not complete authentication in time",
				cfg.Timeout)
		}
		data, err := httpGetJSON(ctx, client, pollURL)
		if err != nil {
			return nil, err
		}
		status, _ := data["status"].(string)
		switch status {
		case "complete":
			token, _ := data["token"].(string)
			if token == "" {
				return nil, oauthErrorf("token poll returned 'complete' but no token: %v", data)
			}
			return &OAuthResult{Token: token, SessionUUID: sessionUUID}, nil
		case "error":
			msg, _ := data["error"].(string)
			if msg == "" {
				msg = "unknown error"
			}
			return nil, oauthErrorf("OAuth flow failed: %s", msg)
		case "not_found":
			return nil, oauthErrorf("OAuth session %s not found: it may have expired", sessionUUID)
		case "pending":
			select {
			case <-ctx.Done():
				return nil, oauthErrorf("OAuth flow canceled: %v", ctx.Err())
			case <-time.After(cfg.PollInterval):
			}
		default:
			return nil, oauthErrorf("unexpected token poll status: %q", status)
		}
	}
}
