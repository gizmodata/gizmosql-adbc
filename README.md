# gizmosql-adbc

[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--adbc-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-adbc)
[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--public-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-public)
[![gizmosql-adbc-ci](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml/badge.svg)](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gizmodata/gizmosql-adbc/go.svg)](https://pkg.go.dev/github.com/gizmodata/gizmosql-adbc/go)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gizmodata/gizmosql-adbc?filename=go%2Fgo.mod)](go/go.mod)
[![Supported Python Versions](https://img.shields.io/pypi/pyversions/adbc-driver-gizmosql)](https://pypi.org/project/adbc-driver-gizmosql/)
[![PyPI version](https://badge.fury.io/py/adbc-driver-gizmosql.svg)](https://badge.fury.io/py/adbc-driver-gizmosql)
[![PyPI Downloads](https://img.shields.io/pepy/dt/adbc-driver-gizmosql.svg)](https://pypi.org/project/adbc-driver-gizmosql/)
[![License](https://img.shields.io/github/license/gizmodata/gizmosql-adbc)](LICENSE)

Native [ADBC](https://arrow.apache.org/adbc/) driver for
[GizmoSQL](https://gizmodata.com/gizmosql), written in Go, with Python
bindings — the successor to
[`adbc-driver-gizmosql`](https://github.com/gizmodata/adbc-driver-gizmosql) 1.x.

> **Status: released.** [`adbc-driver-gizmosql` 2.0](https://pypi.org/project/adbc-driver-gizmosql/)
> ships from this repo — `pip install adbc-driver-gizmosql` gets the
> Go-backed driver with the same API as 1.x
> ([migration guide](docs/migrating-1x-to-2.md)).

## Why a Go driver?

The GizmoSQL-specific behavior that previously lived in the 1.x Python
package — DDL/DML auto-detection and immediate execution (GizmoSQL's
lazy-execution model), `RETURNING` handling, geometry-preserving bulk
ingest, and the OAuth/SSO browser flow — lives in a native Go ADBC
driver built on
[`apache/arrow-adbc`](https://github.com/apache/arrow-adbc)'s Flight SQL
driver. Compiled to a C shared library, one implementation serves
**every** ADBC language: Python, Go, R, C/C++, C#, Rust, and JavaScript.

## Layout

```
go/       Go driver (wraps arrow-adbc's flightsql driver) + cgo C exports
python/   Python bindings — ships libadbc_driver_gizmosql in its wheels,
          keeps the 1.x dbapi.connect() API byte-compatible (PyPI:
          adbc-driver-gizmosql 2.x)
docs/     Design docs, migration guide, conformance results
```

## Features

- `gizmosql://` URI scheme — TLS by default, `?transport=tcp` for plaintext
- DDL/DML auto-detection → immediate server-side execution (DoPut) under
  GizmoSQL's lazy-execution model, with `INSERT/UPDATE/DELETE ... RETURNING`
  eagerly materialized on the query path
- OAuth/SSO code-exchange flow (`/oauth/initiate` → browser →
  `/oauth/token/{uuid}`), including a headless mode — from any language via
  `adbc.gizmosql.*` options
- Server-side query cancellation — abandoning a running query (Ctrl+C,
  `cursor.close()`, interpreter shutdown, `AdbcStatementCancel`) sends
  Flight `CancelFlightInfo` so GizmoSQL interrupts it, like gizmosql-jdbc
- Everything the upstream Flight SQL driver provides: TLS, cookies,
  timeouts, connection profiles, OpenTelemetry tracing and logging
- Python bindings keeping the 1.x `adbc-driver-gizmosql` API
  byte-compatible — the verbatim 1.x test suite is this repo's release
  gate ([migration guide](docs/migrating-1x-to-2.md))

See [docs/plan.md](docs/plan.md) for the design,
[docs/migrating-1x-to-2.md](docs/migrating-1x-to-2.md) for the 1.x → 2.x
migration guide, and
[docs/quickstarts-conformance.md](docs/quickstarts-conformance.md) plus
[docs/downstream-compat.md](docs/downstream-compat.md) for how the
driver is validated.

## Usage (Go)

### Start a GizmoSQL server

Start a GizmoSQL server in Docker, serving the small TPC-H sample
database bundled in the image:

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
           --env DATABASE_FILENAME="data/TPC-H-small.duckdb" \
           --env PRINT_QUERIES="1" \
           --pull missing \
           gizmodata/gizmosql:latest
```

### Install

```bash
go get github.com/gizmodata/gizmosql-adbc/go@latest
```

(The Go module is versioned by `go/vX.Y.Z` tags — its own line,
independent of this repo's Python/release `v*` tags.)

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

The same `driver = "gizmosql"` reference works everywhere the ADBC
driver manager does — with DDL/DML immediacy, `RETURNING` handling,
`gizmosql://` URIs, and OAuth all provided by the shared library.
Verified against the [Columnar ADBC QuickStarts](https://github.com/columnar-tech/adbc-quickstarts)
gizmosql examples (see [docs/quickstarts-conformance.md](docs/quickstarts-conformance.md)):

```go
// Go via the C driver manager (github.com/apache/arrow-adbc/go/adbc/drivermgr)
var drv drivermgr.Driver
db, err := drv.NewDatabase(map[string]string{
    "driver":   "gizmosql",
    "uri":      "gizmosql://localhost:31337",
    "username": "gizmosql_user",
    "password": "gizmosql_password",
})
```

```r
# R
library(adbcdrivermanager)
db <- adbc_database_init(
  adbc_driver("gizmosql"),
  uri = "gizmosql://localhost:31337",
  username = "gizmosql_user",
  password = "gizmosql_password"
)
```

```cpp
// C/C++ (adbc_driver_manager.h)
AdbcDatabaseSetOption(&database, "driver", "gizmosql", &error);
AdbcDatabaseSetOption(&database, "uri", "gizmosql://localhost:31337", &error);
```

Or reference it from a
[connection profile](https://arrow.apache.org/adbc/current/format/connection_profiles.html)
usable in every language:

```toml
profile_version = 1

[Options]
driver = "gizmosql"
uri = "gizmosql://gizmosql.example.com:31337"
username = "gizmosql_user"
password = "{{ env_var(GIZMOSQL_PASSWORD) }}"
```

## Usage (Python)

The Python bindings ship from this repo as
[`adbc-driver-gizmosql`](https://pypi.org/project/adbc-driver-gizmosql/)
(2.x) — the platform wheels bundle the Go driver, and the API is
byte-compatible with the 1.x driver
([migration guide](docs/migrating-1x-to-2.md)):

```bash
pip install adbc-driver-gizmosql
```

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://localhost:31337",  # TLS by default
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True,
                      ) as conn:
    with conn.cursor() as cur:
        # DDL/DML executes immediately (no fetch needed), RETURNING works,
        # and adbc_ingest preserves geometry columns — all driver-side.
        cur.execute("SELECT n_nationkey, n_name FROM nation LIMIT 5")
        print(cur.fetch_arrow_table())
```

OAuth/SSO (`auth_type="external"`), connection profiles (`profile=`),
bulk ingest (`cursor.adbc_ingest`), and Pandas integration all work
exactly as in 1.x.

### Tuning bulk-ingest batch size

`cursor.adbc_ingest` streams each incoming Arrow record batch as one
Flight SQL `DoPut` message, and the driver's gRPC client caps a single
message at **16 MiB** by default. A source that produces large batches
(e.g. the Db2 driver's default of 65,536 rows on a wide table) fails with:

```
InternalError: INTERNAL: [GizmoSQL] [FlightSQL] trying to send message larger
than max (54101430 vs. 16777216) (ResourceExhausted; ExecuteIngest)
```

Two ways to fix it:

1. **Prefer smaller batches at the source** — e.g. `?batch_size=8192` on
   the Db2 URI, or `pyarrow.Table.to_batches(max_chunksize=...)` /
   `RecordBatchReader` slicing for in-memory data. Smaller batches also use
   less memory on both client and server.
2. **Raise the client's gRPC message cap** via `db_kwargs` (the GizmoSQL
   server imposes no receive limit of its own):

```python
with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      db_kwargs={"adbc.flight.sql.client_option.with_max_msg_size": str(256 * 1024 * 1024)},
                      ) as conn:
    ...
```

The same option also raises the *receive* cap for large result batches.

### Query cancellation

A long-running query is cancelled **on the server** (GizmoSQL interrupts
the DuckDB statement) whenever the client walks away from it:

- Ctrl+C / Jupyter "interrupt kernel" while `execute()` or `fetch*()` is
  blocked — `adbc_driver_manager`'s SIGINT handler calls
  `AdbcStatementCancel`, which the driver relays as `CancelFlightInfo`.
- `cursor.adbc_cancel()` from another thread.
- `cursor.close()` / `del cursor` / `conn.close()` / interpreter shutdown
  while the query is still executing.

The client sees an `OperationalError` (the server's `INTERRUPT Error` or a
local `context canceled`), and the connection remains usable. A hard kill
of the process (`kill -9`, OOM) cannot notify the server; that query runs
until it finishes or hits the server's `--query-timeout`.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
