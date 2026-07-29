# gizmosql-adbc

[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--adbc-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-adbc)
[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--public-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-public)
[![gizmosql-adbc-ci](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml/badge.svg)](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gizmodata/gizmosql-adbc/go.svg)](https://pkg.go.dev/github.com/gizmodata/gizmosql-adbc/go)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gizmodata/gizmosql-adbc?filename=go%2Fgo.mod)](go/go.mod)
[![License](https://img.shields.io/github/license/gizmodata/gizmosql-adbc)](LICENSE)

Native [ADBC](https://arrow.apache.org/adbc/) driver for
[GizmoSQL](https://gizmodata.com/gizmosql), written in Go, with Python
bindings — the successor to
[`adbc-driver-gizmosql`](https://github.com/gizmodata/adbc-driver-gizmosql) 1.x.

> **Status: pre-alpha / under active development.** The 1.x Python driver
> remains the supported release until this repo ships
> `adbc-driver-gizmosql` 2.0.

## Why a Go driver?

The GizmoSQL-specific behavior that today lives in the 1.x Python package —
DDL/DML auto-detection and immediate execution (GizmoSQL's lazy-execution
model), `RETURNING` handling, and the OAuth/SSO browser flow — moves into a
native Go ADBC driver built on
[`apache/arrow-adbc`](https://github.com/apache/arrow-adbc)'s Flight SQL
driver. Compiled to a C shared library, one implementation then serves
**every** ADBC language: Python, Go, R, C/C++, C#, Rust, and JavaScript.

## Layout

```
go/       Go driver (wraps arrow-adbc's flightsql driver) + cgo C exports
python/   Python bindings — ships libadbc_driver_gizmosql in its wheels,
          keeps the 1.x dbapi.connect() API byte-compatible (PyPI:
          adbc-driver-gizmosql 2.0)
docs/     Design plan and work plan
```

## Features (planned / ported from 1.x)

- `gizmosql://` URI scheme — TLS by default, `?transport=tcp` for plaintext
- DDL/DML auto-detection → immediate server-side execution (DoPut), with
  `INSERT/UPDATE/DELETE ... RETURNING` eagerly materialized on the query path
- OAuth/SSO code-exchange flow (`/oauth/initiate` → browser →
  `/oauth/token/{uuid}`), including a headless mode
- Everything the upstream Flight SQL driver provides: TLS, cookies,
  timeouts, connection profiles, OpenTelemetry tracing and logging

See [docs/plan.md](docs/plan.md) for the design and
[docs/WORKPLAN.md](docs/WORKPLAN.md) for build-out progress.

## Usage (Go)

### Start a GizmoSQL server

Start a GizmoSQL server in Docker (mounts a small TPC-H database by default):

```bash
docker run --name gizmosql \
           --detach \
           --rm \
           --tty \
           --init \
           --publish 31337:31337 \
           --env TLS_ENABLED="1" \
           --env GIZMOSQL_USERNAME="gizmosql_user" \
           --env GIZMOSQL_PASSWORD="gizmosql_password" \
           --env PRINT_QUERIES="1" \
           --pull missing \
           gizmodata/gizmosql:latest
```

### Install

```bash
go get github.com/gizmodata/gizmosql-adbc/go
```

### Password authentication

```go
package main

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gizmodata/gizmosql-adbc/go/gizmosql"
)

func main() {
	ctx := context.Background()

	drv := gizmosql.NewDriver(memory.DefaultAllocator)
	db, err := drv.NewDatabase(map[string]string{
		"uri":      "gizmosql://localhost:31337", // TLS by default
		"username": "gizmosql_user",
		"password": "gizmosql_password",
		// Development only — trust the demo server's self-signed cert:
		"adbc.flight.sql.client_option.tls_skip_verify": "true",
	})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	cnxn, err := db.Open(ctx)
	if err != nil {
		panic(err)
	}
	defer cnxn.Close()

	stmt, err := cnxn.NewStatement()
	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	if err := stmt.SetSqlQuery(
		"SELECT n_nationkey, n_name FROM nation ORDER BY n_nationkey LIMIT 5",
	); err != nil {
		panic(err)
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		panic(err)
	}
	defer reader.Release()

	for reader.Next() {
		fmt.Println(reader.Record())
	}
}
```

### URI schemes

The preferred way to connect is the `gizmosql://` URI scheme, which is
**secure by default** (gRPC with TLS):

| URI | Meaning |
|---|---|
| `gizmosql://host:31337` | gRPC with TLS (default) |
| `gizmosql://host:31337?transport=tls` | gRPC with TLS (explicit) |
| `gizmosql://host:31337?transport=tcp` | gRPC plaintext (no TLS) |
| `grpc+tls://host:31337` | Legacy TLS spelling (still supported) |
| `grpc+tcp://host:31337` / `grpc://host:31337` | Legacy plaintext spellings (still supported) |
| `flightsql://host:31337` | Upstream Flight SQL spelling (still supported) |

### Observability: OpenTelemetry tracing & logging

Inherited from the upstream Flight SQL driver — trace spans are emitted for
`Database.Open`, `Prepare`, `ExecuteQuery`, and `ExecuteUpdate`:

| Option key (database options) | Description |
|---|---|
| `adbc.telemetry.traces_exporter` | Exporter: `none`, `otlp`, `console`, or `adbcfile` |
| `adbc.telemetry.traces_folder_path` | Output directory for the `adbcfile` exporter |
| `adbc.telemetry.trace_parent` | W3C Trace Context `traceparent` — join an existing distributed trace |

With the `otlp` exporter, the standard `OTEL_EXPORTER_OTLP_*` environment
variables configure the collector endpoint. Structured driver logging is
enabled via `ADBC_DRIVER_FLIGHTSQL_LOG_LEVEL` (`debug`/`info`/`warn`/`error`).

### DDL/DML — auto-detected and executed immediately *(coming in Phase 2)*

GizmoSQL plans queries lazily: the Flight SQL `GetFlightInfo` RPC only
*plans*, so DDL/DML submitted through the normal query path never executes
unless the result is fetched. This driver will detect DDL/DML statements and
route them through `ExecuteUpdate` (DoPut) for immediate server-side
execution — with `INSERT/UPDATE/DELETE ... RETURNING` eagerly materialized
on the query path — matching the behavior of the 1.x Python driver and the
GizmoSQL JDBC/ODBC drivers.

### OAuth/SSO authentication *(coming in Phase 3)*

The 1.x Python driver's browser code-exchange flow
(`/oauth/initiate` → browser → `/oauth/token/{uuid}`) moves into the Go
driver, exposed via `adbc.gizmosql.*` options with a headless mode for
embedded use.

## Usage (Python)

Until 2.0 ships from this repo, use the 1.x driver:
[`pip install adbc-driver-gizmosql`](https://pypi.org/project/adbc-driver-gizmosql/)
— see its [README](https://github.com/gizmodata/adbc-driver-gizmosql#readme)
for full usage (the 2.0 bindings will keep that API byte-compatible).

## License

Apache License 2.0 — see [LICENSE](LICENSE).
