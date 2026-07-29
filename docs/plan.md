# Plan: GizmoSQL ADBC driver in Go

Status: **draft / planning** (2026-07-29)

## Goal

Move the GizmoSQL-specific driver logic that currently lives in this Python
package into a native **Go ADBC driver**, so every ADBC-capable language —
not just Python — gets GizmoSQL's correct behavior. This repo then becomes
the **Python bindings** for that Go driver, exactly the way
`adbc-driver-flightsql` (PyPI) is a thin Python shim that ships the compiled
Go Flight SQL driver (`libadbc_driver_flightsql`) inside its wheels.

## Why

The value this package adds today is implemented in Python only:

1. **DDL/DML auto-detection & immediate execution** — GizmoSQL's
   lazy-execution model means `GetFlightInfo` only *plans* a query; DDL/DML
   submitted through the normal query path never executes unless `DoGet` is
   called. We detect DDL/DML (comment/string-literal-aware keyword scan) and
   route it through `ExecuteUpdate` (`DoPut`), with special eager-materialize
   handling for `... RETURNING` clauses.
2. **OAuth/SSO browser flow** — `/oauth/initiate` → browser → poll
   `/oauth/token/{uuid}`, then Basic Auth with `username="token"`.
3. Assorted hardening (thread-safe `adbc_get_info`, etc.).

Go, R, C/C++, C#, Rust, and JS ADBC users connecting to GizmoSQL get none of
this today. A Go driver puts the logic in one place, beneath every binding.

## Architecture

```
github.com/gizmodata/gizmosql-adbc  (new Go module)
│
├── driver/gizmosql/          Go ADBC driver (implements adbc.Driver)
│     wraps apache/arrow-adbc/go/adbc/driver/flightsql:
│     - delegates transport, TLS, auth, cookies, timeouts, telemetry
│     - intercepts Statement execution for DDL/DML + RETURNING routing
│     - adds OAuth/SSO code-exchange flow (Go stdlib net/http)
│     - accepts gizmosql:// (and flightsql://, grpc+*://) URIs
│     - adbc.gizmosql.* option namespace for driver-specific knobs
│
├── pkg/                      cgo exports → C shared library
│     libadbc_driver_gizmosql.{so,dylib,dll}
│     (reuse upstream's go/adbc/pkg template machinery for the C ABI)
│
└── (optional) cmd/           small CLI for smoke-testing / token retrieval
```

Consumers:

- **Python** (`adbc-driver-gizmosql`, this repo): wheels ship the compiled
  shared library per platform; `dbapi.connect()` keeps its exact public API
  (`auth_type="external"`, `tls_skip_verify=`, `profile=`, ...) but becomes a
  thin option-mapping layer over the driver manager — the DDL/DML cursor
  logic and OAuth flow are deleted from Python once the Go driver covers
  them.
- **Go**: native `import "…/driver/gizmosql"` — no shim needed.
- **R / C++ / C# / Rust / JS**: load `libadbc_driver_gizmosql` via each
  language's ADBC driver manager.
- **Everything**: an ADBC **driver manifest** so
  `driver = "gizmosql"` works in connection profiles across all languages.

## Key design decisions to settle

- **Repo layout** (decided 2026-07-29): new `gizmodata/gizmosql-adbc`
  **monorepo** modeled on `apache/arrow-adbc` itself — `go/` (driver + cgo
  exports) and `python/` (the 2.0 bindings that ship the shared library in
  their wheels) side by side, released in lockstep by one pipeline. The
  Python 2.0 sources move to the new repo; the PyPI package name
  `adbc-driver-gizmosql` is unchanged. This repo remains for 1.x
  maintenance and gets a pointer README once 2.0 ships.
- **DDL/DML interception point**: implement `adbc.Statement` wrapping the
  flightsql statement — on `ExecuteQuery`, run the keyword scan; DDL/DML
  goes to `ExecuteUpdate` under the hood, `RETURNING` takes the query path
  with eager materialization. Port `_is_ddl_dml()` / `_has_returning()`
  semantics (comment + string-literal stripping) verbatim, with the Python
  unit tests translated to Go table tests.
- **OAuth UX in Go**: browser open (`open`/`xdg-open`/`rundll32`) + polling
  is fine from Go for CLI/desktop use; also expose a "give me the URL,
  I'll poll" mode (options out / callback) for embedded and headless hosts.
  Keep `get_oauth_token()` in Python as a wrapper over a driver action (or
  retain the stdlib implementation for that one entry point).
- **Option namespace**: `adbc.gizmosql.auth_type`,
  `adbc.gizmosql.oauth.url`, `adbc.gizmosql.oauth.timeout_seconds`, ... —
  mirroring the connect() kwargs 1:1 so profiles can express everything.
- **Upstreaming**: long-term, consider proposing the driver to
  `apache/arrow-adbc` as a community driver; start under gizmodata to move
  fast.

## Phases

1. **Scaffold** — Go module; pass-through driver delegating 100% to
   upstream flightsql; integration tests against a GizmoSQL test server
   (reuse the `gizmosql` PyPI server harness or a Docker fixture in CI).
2. **DDL/DML + RETURNING** — port detection/routing with Go table tests
   mirroring `tests/test_connect_unit.py` cases and the `RETURNING`
   integration tests.
3. **OAuth/SSO** — port the code-exchange flow; headless mode included.
4. **C shared library + release matrix** — cgo exports via upstream's pkg
   templates; GitHub Actions build matrix (linux amd64/arm64 + manylinux,
   macOS arm64/x86_64, windows amd64); attach binaries to GitHub Releases
   (per our standard release pattern), publish a driver manifest.
5. **Python bindings switchover** — this repo consumes the shared library
   in its wheels (per-platform wheel builds), keeps the public API
   byte-compatible, and drops the Python-side cursor/OAuth logic. Ship as
   a major version (2.0.0) after a deprecation overlap release.
6. **Other bindings** — document R/C#/Rust/JS usage via driver manager +
   manifest; examples in the GizmoSQL docs.

## Risks / open questions

- **Upstream API churn**: the Go `driverbase`/flightsql internals are not a
  stable public API in the way the C ABI is — pin versions, keep the
  wrapper surface small (Statement interception + options), track upstream
  releases (we already do this for the Python floor).
- **Build/packaging cost**: CGO cross-compilation matrix and per-platform
  wheels are the bulk of the engineering effort (upstream's CI is the
  reference implementation).
- **Behavior parity**: the Python DDL/DML detector has accumulated
  hard-won fixes (dbt comment prefixes, `RETURNING` in string literals,
  eager materialization guarantees). The Go port must carry the full test
  suite over, not just the happy path.
- **Wheel size**: shipping our own shared library roughly doubles the
  native payload vs. reusing upstream's (we'd no longer need upstream's
  wheel at runtime — dependency becomes `adbc-driver-manager` only).
