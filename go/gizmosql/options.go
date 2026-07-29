// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// GizmoSQL-specific database option keys. They mirror the 1.x Python
// driver's connect() keyword arguments so connection profiles can
// express everything the Python API could.
const (
	// OptionAuthType selects the authentication mode: "password"
	// (default; username/password pass through to Basic Auth) or
	// "external" (OAuth/SSO browser flow).
	OptionAuthType = "adbc.gizmosql.auth_type"

	OptionValueAuthTypePassword = "password"
	OptionValueAuthTypeExternal = "external"

	// OptionOAuthURL is an explicit OAuth base URL, e.g.
	// "https://gizmosql.example.com:31339". When unset, the OAuth server
	// is discovered by probing the connection host (HTTPS then HTTP).
	OptionOAuthURL = "adbc.gizmosql.oauth.url"

	// OptionOAuthPort is the OAuth HTTP port for discovery
	// (default "31339").
	OptionOAuthPort = "adbc.gizmosql.oauth.port"

	// OptionOAuthTimeoutSeconds bounds the whole flow (default "300").
	OptionOAuthTimeoutSeconds = "adbc.gizmosql.oauth.timeout_seconds"

	// OptionOAuthPollIntervalSeconds is the delay between token polls
	// (default "1").
	OptionOAuthPollIntervalSeconds = "adbc.gizmosql.oauth.poll_interval_seconds"

	// OptionOAuthOpenBrowser controls opening the local browser
	// ("true"/"false", default "true"). With "false", the auth URL is
	// printed to stderr — the headless mode.
	OptionOAuthOpenBrowser = "adbc.gizmosql.oauth.open_browser"

	// OptionOAuthTLSSkipVerify skips TLS verification for the OAuth
	// HTTP server. Defaults to the Flight SQL client's tls_skip_verify
	// setting.
	OptionOAuthTLSSkipVerify = "adbc.gizmosql.oauth.tls_skip_verify"

	// flightSQLTLSSkipVerify is the upstream driver's option key.
	flightSQLTLSSkipVerify = "adbc.flight.sql.client_option.tls_skip_verify"
)

// applyGizmoSQLOptions consumes the adbc.gizmosql.* options from opts
// (the upstream driver rejects unknown option keys), running the OAuth
// flow when auth_type is "external" and injecting the resulting token as
// Basic Auth credentials. Returns the remaining options for the upstream
// driver. The input map is not mutated.
func applyGizmoSQLOptions(ctx context.Context, opts map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(opts))
	own := make(map[string]string)
	for k, v := range opts {
		if strings.HasPrefix(k, "adbc.gizmosql.") {
			own[k] = v
			continue
		}
		out[k] = v
	}

	authType := own[OptionAuthType]
	switch authType {
	case "", OptionValueAuthTypePassword:
		return out, nil
	case OptionValueAuthTypeExternal:
	default:
		return nil, adbc.Error{
			Code: adbc.StatusInvalidArgument,
			Msg: "[GizmoSQL] invalid " + OptionAuthType + " value " +
				strconv.Quote(authType) + ": must be \"password\" or \"external\"",
		}
	}

	cfg := OAuthConfig{
		BaseURL:     own[OptionOAuthURL],
		OpenBrowser: true,
	}

	if cfg.BaseURL == "" {
		host := extractHost(out["uri"])
		if host == "" {
			return nil, adbc.Error{
				Code: adbc.StatusInvalidArgument,
				Msg: "[GizmoSQL] " + OptionAuthType + "=external requires either a " +
					"connection uri with a host or an explicit " + OptionOAuthURL,
			}
		}
		cfg.Host = host
	}

	if v := own[OptionOAuthPort]; v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, optionParseError(OptionOAuthPort, v)
		}
		cfg.Port = port
	}
	if v := own[OptionOAuthTimeoutSeconds]; v != "" {
		secs, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, optionParseError(OptionOAuthTimeoutSeconds, v)
		}
		cfg.Timeout = time.Duration(secs * float64(time.Second))
	}
	if v := own[OptionOAuthPollIntervalSeconds]; v != "" {
		secs, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, optionParseError(OptionOAuthPollIntervalSeconds, v)
		}
		cfg.PollInterval = time.Duration(secs * float64(time.Second))
	}
	if v := own[OptionOAuthOpenBrowser]; v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, optionParseError(OptionOAuthOpenBrowser, v)
		}
		cfg.OpenBrowser = b
	}
	// OAuth TLS verification follows the Flight SQL client's setting
	// unless overridden explicitly.
	if v := own[OptionOAuthTLSSkipVerify]; v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, optionParseError(OptionOAuthTLSSkipVerify, v)
		}
		cfg.TLSSkipVerify = b
	} else if v := out[flightSQLTLSSkipVerify]; v != "" {
		cfg.TLSSkipVerify, _ = strconv.ParseBool(v)
	}

	result, err := GetOAuthToken(ctx, cfg)
	if err != nil {
		return nil, adbc.Error{Code: adbc.StatusUnauthenticated, Msg: "[GizmoSQL] " + err.Error()}
	}
	// GizmoSQL validates external identity tokens presented as Basic
	// Auth with the fixed username "token".
	out["username"] = "token"
	out["password"] = result.Token
	return out, nil
}

func optionParseError(key, value string) error {
	return adbc.Error{
		Code: adbc.StatusInvalidArgument,
		Msg:  "[GizmoSQL] invalid value " + strconv.Quote(value) + " for option " + key,
	}
}

// extractHost pulls the hostname out of a connection URI, tolerating
// schemes, ports, paths, and query strings — parity with the 1.x Python
// driver's _extract_host.
func extractHost(uri string) string {
	if uri == "" {
		return ""
	}
	remainder := uri
	if idx := strings.Index(remainder, "://"); idx != -1 {
		remainder = remainder[idx+len("://"):]
	}
	// Strip path and query string (e.g. gizmosql://host:port?transport=tcp).
	if idx := strings.IndexAny(remainder, "/?"); idx != -1 {
		remainder = remainder[:idx]
	}
	// Strip port.
	if idx := strings.LastIndex(remainder, ":"); idx != -1 {
		remainder = remainder[:idx]
	}
	return remainder
}
