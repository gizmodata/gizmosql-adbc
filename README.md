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

### DDL/DML — auto-detected and executed immediately

GizmoSQL plans queries lazily: the Flight SQL `GetFlightInfo` RPC only
*plans*, so DDL/DML submitted through the normal query path never executes
unless the result is fetched. This driver detects DDL/DML statements
(first keyword, comments stripped) and routes them through `ExecuteUpdate`
(DoPut) for immediate server-side execution — no fetch required.
`INSERT/UPDATE/DELETE ... RETURNING` takes the query path with the result
**eagerly materialized**, so the DML fires even if you never read the
returned reader — matching the 1.x Python driver and the GizmoSQL
JDBC/ODBC drivers:

```go
stmt.SetSqlQuery("CREATE TABLE t (id INT)")
_, _, _ = stmt.ExecuteQuery(ctx) // executes immediately via DoPut

stmt.SetSqlQuery("INSERT INTO t VALUES (1), (2)")
_, affected, _ := stmt.ExecuteQuery(ctx) // affected == 2, already executed
```

Routing applies to plain SQL only — statements with bound parameters
(`Bind`/`BindStream`) or Substrait plans use standard prepared-statement
semantics.

### OAuth/SSO authentication

When your GizmoSQL server is configured with OAuth, set
`adbc.gizmosql.auth_type` to `external` — the driver initiates the flow,
opens your browser to the identity provider, polls for completion, and
connects with the identity token via Basic Auth (username `token`):

```go
db, err := drv.NewDatabase(map[string]string{
	"uri":                     "gizmosql://gizmosql.example.com:31337",
	"adbc.gizmosql.auth_type": "external",
})
```

| Option key | Default | Description |
|---|---|---|
| `adbc.gizmosql.auth_type` | `password` | `password` or `external` (OAuth/SSO) |
| `adbc.gizmosql.oauth.url` | *(discovered)* | Explicit OAuth base URL; otherwise probed from the connection host (HTTPS, then HTTP) |
| `adbc.gizmosql.oauth.port` | `31339` | OAuth HTTP port used for discovery |
| `adbc.gizmosql.oauth.timeout_seconds` | `300` | Max seconds to wait for the user to complete auth |
| `adbc.gizmosql.oauth.poll_interval_seconds` | `1` | Delay between token polls |
| `adbc.gizmosql.oauth.open_browser` | `true` | `false` prints the auth URL to stderr instead (headless) |
| `adbc.gizmosql.oauth.tls_skip_verify` | *(follows Flight SQL setting)* | Skip TLS verification for the OAuth HTTP server |

Go-native callers can also run the flow directly — including fully
headless with a custom URL handler — via `gizmosql.GetOAuthToken`:

```go
result, err := gizmosql.GetOAuthToken(ctx, gizmosql.OAuthConfig{
	Host:           "gizmosql.example.com",
	AuthURLHandler: func(u string) { fmt.Println("authenticate at:", u) },
})
// result.Token → use as password with username "token"
```

## Usage (any language, via driver manifest)

Build (or download from a release) the shared library, then install a
[driver manifest](https://arrow.apache.org/adbc/current/format/driver_manifests.html)
so every ADBC driver manager can load the driver **by name**:

```bash
make -C go lib   # produces go/build/libadbc_driver_gizmosql.{so,dylib,dll}
```

Copy `packaging/gizmosql.toml.in` to a driver search path as
`gizmosql.toml` (e.g. `~/.config/adbc/drivers/` on Linux,
`~/Library/Application Support/ADBC/Drivers/` on macOS, or any directory
in `ADBC_DRIVER_PATH`), filling in the library path. Then:

```python
# Python — no GizmoSQL-specific package needed:
import adbc_driver_manager.dbapi as dbapi

with dbapi.connect(driver="gizmosql", db_kwargs={
    "uri": "gizmosql://localhost:31337",
    "username": "gizmosql_user",
    "password": "gizmosql_password",
}) as conn:
    ...
```

The same `driver = "gizmosql"` reference works from C/C++, R, C#, Rust,
and in [connection profiles](https://arrow.apache.org/adbc/current/format/connection_profiles.html)
— with DDL/DML immediacy, `RETURNING` handling, `gizmosql://` URIs, and
OAuth all provided by the shared library.

## Usage (Python)

Until 2.0 ships from this repo, use the 1.x driver:
[`pip install adbc-driver-gizmosql`](https://pypi.org/project/adbc-driver-gizmosql/)
— see its [README](https://github.com/gizmodata/adbc-driver-gizmosql#readme)
for full usage (the 2.0 bindings will keep that API byte-compatible).

## License

Apache License 2.0 — see [LICENSE](LICENSE).
