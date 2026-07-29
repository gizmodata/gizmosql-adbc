// Licensed under the Apache License, Version 2.0.

package gizmosql

import "testing"

func TestRewriteURI(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"gizmosql basic", "gizmosql://localhost:31337", "flightsql://localhost:31337"},
		{
			"gizmosql with transport tcp",
			"gizmosql://localhost:31337?transport=tcp",
			"flightsql://localhost:31337?transport=tcp",
		},
		{
			"gizmosql with transport tls",
			"gizmosql://host.example.com:31337?transport=tls",
			"flightsql://host.example.com:31337?transport=tls",
		},
		{"flightsql untouched", "flightsql://localhost:31337", "flightsql://localhost:31337"},
		{"grpc+tls untouched", "grpc+tls://localhost:31337", "grpc+tls://localhost:31337"},
		{"grpc+tcp untouched", "grpc+tcp://localhost:31337", "grpc+tcp://localhost:31337"},
		{"grpc untouched", "grpc://localhost:31337", "grpc://localhost:31337"},
		{"empty untouched", "", ""},
		{
			"scheme only at start",
			"grpc+tls://gizmosql://weird",
			"grpc+tls://gizmosql://weird",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RewriteURI(tc.in); got != tc.want {
				t.Errorf("RewriteURI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRewriteOptions(t *testing.T) {
	in := map[string]string{
		"uri":      "gizmosql://localhost:31337?transport=tcp",
		"username": "u",
	}
	out := rewriteOptions(in)
	if out["uri"] != "flightsql://localhost:31337?transport=tcp" {
		t.Errorf("uri not rewritten: %q", out["uri"])
	}
	if out["username"] != "u" {
		t.Errorf("unrelated option changed: %q", out["username"])
	}
	if in["uri"] != "gizmosql://localhost:31337?transport=tcp" {
		t.Errorf("input map mutated: %q", in["uri"])
	}
}

func TestRewriteOptionsNil(t *testing.T) {
	if rewriteOptions(nil) != nil {
		t.Error("nil map should stay nil")
	}
	out := rewriteOptions(map[string]string{"username": "u"})
	if _, ok := out["uri"]; ok {
		t.Error("uri key should not be invented")
	}
}
