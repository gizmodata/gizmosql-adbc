# adbc-driver-gizmosql

A Python [ADBC](https://arrow.apache.org/adbc/) driver for
[GizmoSQL](https://gizmodata.com/gizmosql) with OAuth/SSO support —
powered by a native Go driver core bundled in the wheel.

[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--adbc-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-adbc)
[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--public-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-public)
[![gizmosql-adbc-ci](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml/badge.svg)](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml)
[![Supported Python Versions](https://img.shields.io/pypi/pyversions/adbc-driver-gizmosql)](https://pypi.org/project/adbc-driver-gizmosql/)
[![PyPI version](https://badge.fury.io/py/adbc-driver-gizmosql.svg)](https://badge.fury.io/py/adbc-driver-gizmosql)
[![PyPI Downloads](https://img.shields.io/pepy/dt/adbc-driver-gizmosql.svg)](https://pypi.org/project/adbc-driver-gizmosql/)

## Overview

`adbc-driver-gizmosql` connects Python to GizmoSQL over Arrow Flight SQL
with GizmoSQL-specific features built in:

- **OAuth/SSO browser flow** — Authenticate via your identity provider
  (Google, Okta, etc.) with a single parameter change
- **DBAPI 2.0 interface** — `connect()` / cursors / `fetch_arrow_table()`,
  with the same API as the 1.x driver
  ([migration guide](https://github.com/gizmodata/gizmosql-adbc/blob/main/docs/migrating-1x-to-2.md))
- **DDL/DML auto-detection** — `CREATE`/`INSERT`/`UPDATE`/`DELETE`
  execute immediately on the server (GizmoSQL plans queries lazily);
  `... RETURNING` is eagerly materialized
- **Geometry-aware bulk ingest** — `cursor.adbc_ingest` preserves
  `GEOMETRY` columns instead of degrading them to `BLOB`
- **Minimal dependencies** — Only `adbc-driver-manager` and `pyarrow`;
  the native driver library (written in Go, built on
  [`apache/arrow-adbc`](https://github.com/apache/arrow-adbc)'s Flight
  SQL driver) ships inside the platform wheel
- **OpenTelemetry tracing & structured logging** — see
  [Observability](#observability-opentelemetry-tracing--logging)

## Install

```shell
# Create and activate a virtual environment
python3 -m venv .venv
. .venv/bin/activate

pip install adbc-driver-gizmosql
```

Platform wheels are published for Linux (amd64/arm64 manylinux), macOS
(arm64/amd64), and Windows (amd64/arm64). To develop from source, see
the [repository](https://github.com/gizmodata/gizmosql-adbc).

## Usage

### Start a GizmoSQL server

First — start a GizmoSQL server in Docker, serving the small TPC-H
sample database bundled in the image:

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

### Password authentication

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True,
                      ) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT n_nationkey, n_name FROM nation WHERE n_nationkey = ?",
                    parameters=[24])
        table = cur.fetch_arrow_table()
        print(table)
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

The scheme is handled inside the driver library, so it also works in
[connection profiles](#connection-profiles).

### DDL/DML — auto-detected and executed immediately

GizmoSQL plans queries lazily, so DDL/DML submitted through the normal
query path would never execute unless the result is fetched.
`cursor.execute()` automatically detects DDL/DML statements and executes
them immediately on the server, matching the behavior of the GizmoSQL
JDBC and ODBC drivers. No special API is needed — just use `execute()`
for everything:

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True,
                      ) as conn:
    with conn.cursor() as cur:
        # DDL and DML work with regular execute()
        cur.execute("CREATE TABLE t (a INT)")
        cur.execute("INSERT INTO t VALUES (1)")

        # SELECT works as usual
        cur.execute("SELECT * FROM t")
        print(cur.fetch_arrow_table())

        # RETURNING is eagerly materialized — the DML fires even if
        # you never read the result
        cur.execute("DELETE FROM t WHERE a = 1 RETURNING a")
        print(cur.fetch_arrow_table())

        # Cleanup
        cur.execute("DROP TABLE t")
```

> **Note:** `cursor.execute_update(query)` is still available if you need
> the rows-affected count returned directly:
> `rows = cur.execute_update("INSERT ...")`.

### OAuth/SSO authentication

When your GizmoSQL server is configured with OAuth, simply change
`auth_type`:

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://gizmosql.example.com:31337",
                      auth_type="external",
                      tls_skip_verify=True,
                      ) as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT CURRENT_USER AS user")
        print(cur.fetch_arrow_table())
```

This will:
1. Auto-discover the OAuth server endpoint
2. Open your browser to the identity provider login page
3. Poll for completion and retrieve the identity token
4. Connect to GizmoSQL using the token via Basic Auth (`username="token"`)

### Connection profiles

This driver supports [ADBC connection profiles](https://arrow.apache.org/adbc/current/format/connection_profiles.html)
— reusable TOML files that bundle the server URI and options so
connection code stays credential-free.

Create a profile, e.g. `~/.config/adbc/profiles/gizmosql_dev.toml` on
Linux, `~/Library/Application Support/ADBC/Profiles/gizmosql_dev.toml`
on macOS, or any directory listed in the `ADBC_PROFILE_PATH` environment
variable:

```toml
profile_version = 1

[Options]
uri = "gizmosql://gizmosql.example.com:31337"
username = "gizmosql_username"
# Keep secrets out of the file — substituted from the environment at connect time
password = "{{ env_var(GIZMOSQL_PASSWORD) }}"
```

Then connect by profile name (no `uri` needed):

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect(profile="gizmosql_dev") as conn:
    with conn.cursor() as cur:
        cur.execute("SELECT 1 AS value")
        print(cur.fetch_arrow_table())
```

Notes:

- `connect("profile://gizmosql_dev")` and `connect(profile="/abs/path/to/profile.toml")`
  work too.
- The profile does **not** need a `driver` entry — the driver bundled
  with this package is supplied automatically.
- Options passed explicitly to `connect()` (e.g. `username=`, `password=`,
  `db_kwargs=`) take precedence over the profile's `[Options]`.
- Boolean/typed driver options are plain strings in profiles, e.g.
  `"adbc.flight.sql.client_option.tls_skip_verify" = "true"` (dotted keys
  must be quoted in TOML).

### Advanced: Standalone OAuth token retrieval

```python
from adbc_driver_gizmosql import get_oauth_token

result = get_oauth_token(
    host="gizmosql.example.com",
    port=31339,           # OAuth HTTP port (default)
    tls_skip_verify=True, # Skip TLS cert verification
    timeout=300,          # Seconds to wait for user to complete auth
)

print(f"Token: {result.token}")
print(f"Session: {result.session_uuid}")
```

### Bulk ingest (load Arrow data into a table)

The ADBC `adbc_ingest` method on the cursor lets you load Arrow tables,
record batches, or record batch readers directly into GizmoSQL — no
row-by-row INSERT needed:

```python
import pyarrow as pa
from adbc_driver_gizmosql import dbapi as gizmosql

# Build an Arrow table
table = pa.table({
    "id": [1, 2, 3],
    "name": ["Alice", "Bob", "Charlie"],
    "score": [95.0, 87.5, 91.2],
})

with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True,
                      ) as conn:
    with conn.cursor() as cur:
        # Create a new table and insert the data
        cur.adbc_ingest("students", table, mode="create")

        # Verify
        cur.execute("SELECT * FROM students")
        print(cur.fetch_arrow_table())
```

Supported modes: `"create"`, `"append"`, `"replace"`, `"create_append"`.

Data containing `geoarrow.*` extension columns (as produced by fetching
GizmoSQL `GEOMETRY` columns) round-trips with the geometry type
preserved — in every mode.

### Observability: OpenTelemetry tracing & logging

The driver emits [OpenTelemetry](https://opentelemetry.io/) trace spans
for the core operations — `Database.Open`, `Prepare`, `ExecuteQuery`,
and `ExecuteUpdate` — and supports structured logging.

#### Tracing

Enable a trace exporter via `db_kwargs` (per-connection) or the standard
`OTEL_*` environment variables (process-wide):

```python
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True,
                      db_kwargs={
                          # one of: none | otlp | console | adbcfile
                          "adbc.telemetry.traces_exporter": "otlp",
                      },
                      ) as conn:
    ...
```

| Option key (`db_kwargs`) | Description |
|---|---|
| `adbc.telemetry.traces_exporter` | Exporter: `none`, `otlp`, `console`, or `adbcfile` |
| `adbc.telemetry.traces_folder_path` | Output directory when using the `adbcfile` exporter |
| `adbc.telemetry.trace_parent` | W3C Trace Context `traceparent` — join your application's existing distributed trace |

With the `otlp` exporter, the standard OpenTelemetry environment
variables (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`,
...) configure the collector endpoint. `OTEL_TRACES_EXPORTER` may also
be used instead of the database option (the option wins when both are
set).

#### Driver logging

Set the log level for the underlying Flight SQL driver via an
environment variable (great for debugging connection/TLS/auth issues):

```bash
export ADBC_DRIVER_FLIGHTSQL_LOG_LEVEL=debug   # debug | info | warn | error
```

### Pandas integration

```python
import pandas as pd
from adbc_driver_gizmosql import dbapi as gizmosql

with gizmosql.connect("gizmosql://localhost:31337",
                      username="gizmosql_user",
                      password="gizmosql_password",
                      tls_skip_verify=True,
                      ) as conn:
    df = pd.read_sql("SELECT * FROM nation ORDER BY n_nationkey", conn)
    print(df)
```

## API Reference

### `dbapi.connect()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `uri` | `str` | `None` | Server URI (e.g., `"gizmosql://host:31337"` — TLS by default; `grpc+tls://`, `grpc+tcp://`, and `flightsql://` also accepted); optional if `profile` supplies it. `"profile://<name>"` is also accepted |
| `profile` | `str` | `None` | ADBC connection profile — a bare name resolved via the standard search paths (incl. `ADBC_PROFILE_PATH`) or an absolute path to a `.toml` file. At least one of `uri`/`profile` is required |
| `username` | `str` | `None` | Username for password auth |
| `password` | `str` | `None` | Password for password auth |
| `tls_skip_verify` | `bool` | `False` | Skip TLS cert verification |
| `auth_type` | `str` | `"password"` | `"password"` or `"external"` (OAuth) |
| `oauth_port` | `int` | `31339` | OAuth HTTP server port |
| `oauth_url` | `str` | `None` | Explicit OAuth base URL |
| `oauth_tls_skip_verify` | `bool` | `None` | TLS skip for OAuth (defaults to `tls_skip_verify`) |
| `oauth_timeout` | `int` | `300` | Seconds to wait for OAuth |
| `open_browser` | `bool` | `True` | Auto-open browser for OAuth |
| `db_kwargs` | `dict` | `None` | Extra ADBC database options |
| `conn_kwargs` | `dict` | `None` | Extra ADBC connection options |
| `autocommit` | `bool` | `True` | Enable autocommit |

### `cursor.execute_update()`

Execute a DDL/DML statement immediately and return the rows-affected
count. This is an alternative to `cursor.execute()` when you need the
rows-affected count as the return value.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `query` | `str` | *required* | SQL DDL or DML statement to execute |

Returns: `int` — number of rows affected (`0` for DDL statements that do
not affect rows)

> **Note:** `cursor.execute()` auto-detects DDL/DML and executes it
> immediately, so `execute_update()` is only needed when you want the
> rows-affected count returned directly. The module-level
> `gizmosql.execute_update(cursor, query)` function is still available
> for backward compatibility.

### `get_oauth_token()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `host` | `str` | *required* | GizmoSQL server hostname |
| `port` | `int` | `31339` | OAuth HTTP port |
| `tls_skip_verify` | `bool` | `True` | Skip TLS cert verification |
| `timeout` | `int` | `300` | Seconds to wait |
| `poll_interval` | `float` | `1` | Seconds between polls |
| `open_browser` | `bool` | `True` | Auto-open browser |
| `oauth_url` | `str` | `None` | Explicit OAuth base URL |

Returns: `OAuthResult(token=str, session_uuid=str)`

## How the OAuth flow works

```
Python Client              GizmoSQL OAuth Server         Identity Provider
     |                            |                            |
     +-- GET /oauth/initiate ---->|                            |
     |<-- {uuid, auth_url} -------|                            |
     |                            |                            |
     +-- Open browser to auth_url-|--------------------------->|
     |                            |                            |
     |                            |<-- callback (auth code) ---|
     |                            |-- exchange code for token ->|
     |                            |<-- id_token ---------------|
     |                            |                            |
     +-- GET /oauth/token/{uuid}->|                            |
     |<-- {status: complete,      |                            |
     |     token: <id_token>}     |                            |
     |                            |                            |
     +-- Flight BasicAuth ------->|                            |
     |   user="token"             | (verify token via JWKS,    |
     |   pass=<id_token>          |  issue server JWT)         |
     |<-- Server Bearer token ----|                            |
```

## Under the hood

The wheel bundles `libadbc_driver_gizmosql`, a native driver written in
Go on top of [`apache/arrow-adbc`](https://github.com/apache/arrow-adbc)'s
Flight SQL driver, loaded via `adbc-driver-manager`. The same library is
usable from Go, R, C/C++, C#, Rust, and JavaScript — see the
[repository README](https://github.com/gizmodata/gizmosql-adbc) for
non-Python usage. To point at a custom driver build, set
`GIZMOSQL_DRIVER_LIB=/path/to/libadbc_driver_gizmosql.<ext>`.

Upgrading from the 1.x pure-Python driver? The API is byte-compatible —
see the
[migration guide](https://github.com/gizmodata/gizmosql-adbc/blob/main/docs/migrating-1x-to-2.md).

## License

Apache License 2.0 — see
[LICENSE](https://github.com/gizmodata/gizmosql-adbc/blob/main/LICENSE).
