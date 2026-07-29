// Licensed under the Apache License, Version 2.0.

package gizmosql

import "strings"

const (
	// SchemeGizmoSQL is this driver's preferred URI scheme. It maps 1:1
	// onto the upstream Flight SQL driver's flightsql:// scheme: secure
	// TLS by default, with ?transport=tcp for plaintext and
	// ?transport=unix for Unix domain sockets.
	SchemeGizmoSQL = "gizmosql://"

	schemeFlightSQL = "flightsql://"
)

// RewriteURI translates a gizmosql:// URI into the flightsql:// URI
// understood by the upstream driver, preserving host, port, path, and
// query parameters. Any other URI (flightsql://, grpc+tls://, grpc+tcp://,
// grpc://, grpc+unix://) is returned unchanged.
func RewriteURI(uri string) string {
	if strings.HasPrefix(uri, SchemeGizmoSQL) {
		return schemeFlightSQL + uri[len(SchemeGizmoSQL):]
	}
	return uri
}

// rewriteOptions returns a copy of opts with the "uri" option's
// gizmosql:// scheme rewritten. The input map is never mutated; a nil
// map is returned as nil.
func rewriteOptions(opts map[string]string) map[string]string {
	if opts == nil {
		return nil
	}
	out := make(map[string]string, len(opts))
	for k, v := range opts {
		out[k] = v
	}
	if uri, ok := out["uri"]; ok {
		out["uri"] = RewriteURI(uri)
	}
	return out
}
