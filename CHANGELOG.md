# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

Versioning note: this repo's releases continue the `adbc-driver-gizmosql`
line — the first release from this repo will be 2.0.0 (the Go driver plus
Python bindings), succeeding the 1.x pure-Python driver.

## [Unreleased]

### Added
- Repository scaffold: Go driver + Python bindings monorepo layout,
  design plan (`docs/plan.md`), and work plan (`docs/WORKPLAN.md`).
- Go module `github.com/gizmodata/gizmosql-adbc/go` pinned to
  `arrow-adbc/go/adbc` v1.12.0: `gizmosql.NewDriver(alloc)` pass-through
  driver wrapping the upstream Flight SQL driver with Database /
  Connection / Statement interception points, and the `gizmosql://` URI
  scheme (TLS by default, `?transport=tcp` for plaintext) rewritten onto
  `flightsql://`. Unit tests cover the URI rewrite, option-map
  non-mutation, downstream delegation (via a recording fake), and
  real-driver option validation.
- Go live-server integration tests: a harness that discovers or reuses a
  `gizmosql_server` binary, mints a self-signed TLS certificate in Go,
  and verifies `SELECT 1` over `gizmosql://` plus legacy `grpc+tls://`
  parity against a real server.
- GitHub Actions CI (`ci.yml`): gofmt / go vet / unit tests / live-server
  integration tests on ubuntu and macos.
- DDL/DML auto-detection and immediate execution (Phase 2): the statement
  wrapper classifies plain SQL by first keyword (comments stripped,
  string-literal-aware `RETURNING` carve-out — ported verbatim from the
  1.x Python driver) and routes DDL/DML issued via `ExecuteQuery` through
  `ExecuteUpdate` (DoPut) so it executes immediately despite GizmoSQL's
  lazy planning; `INSERT/UPDATE/DELETE ... RETURNING` takes the query
  path with eager materialization so the DML fires even if the caller
  never reads the result. `GetInfo` is mutex-serialized on the connection
  (apache/arrow-adbc#1178 parity). Unit tests mirror
  `test_sql_routing_unit.py`; live-server tests cover
  execute-without-fetch, RETURNING persistence and row delivery, and
  streaming SELECTs.
- OAuth/SSO authentication (Phase 3): `GetOAuthToken` ports the 1.x
  browser code-exchange flow (`/oauth/initiate` → browser → poll
  `/oauth/token/{uuid}`) to Go stdlib with context cancellation, HTTPS→
  HTTP endpoint discovery, and a headless mode (stderr URL or
  `AuthURLHandler` callback). New `adbc.gizmosql.*` database options
  (`auth_type=external`, `oauth.url/.port/.timeout_seconds/
  .poll_interval_seconds/.open_browser/.tls_skip_verify`) run the flow at
  database creation and inject the identity token as Basic Auth
  (username "token"). Unit tests mirror 1.x `test_oauth_unit.py` against
  mock OAuth HTTP servers.
- C shared library (Phase 4, first slice): `go/pkg/gizmosql` cgo export
  package (adapted from apache/arrow-adbc's generated flightsql export
  package, Apache-2.0 headers retained) builds
  `libadbc_driver_gizmosql.{so,dylib,dll}` via `make -C go lib`,
  exporting `AdbcDriverInit` (standard entrypoint) plus GizmoSQL-named
  aliases. `python/smoke_test_dylib.py` verifies the full C-ABI path from
  Python's stock `adbc_driver_manager` against a live server: gizmosql://
  URI, immediate DDL/DML, and RETURNING persistence — all served from
  the Go driver inside the shared library.
- Phase 4 (second slice): ADBC driver-manifest template
  (`packaging/gizmosql.toml.in`, entrypoint `AdbcDriverInit`) so
  `driver = "gizmosql"` resolves by name in every driver manager; CI now
  builds the shared library on linux amd64/arm64, macOS arm64/amd64, and
  windows amd64 and runs the Python C-ABI smoke test on linux+macos; a
  release workflow packages per-platform tarballs and creates a GitHub
  Release with CHANGELOG-extracted notes on v* tag pushes.
- Python bindings 2.0.0.dev0 (Phase 5, first slice): `python/` package
  `adbc-driver-gizmosql` keeps the 1.x public API byte-compatible
  (`dbapi.connect()` with profiles/OAuth/TLS kwargs, `execute_update`,
  `get_oauth_token`, vendored `DatabaseOptions`/`ConnectionOptions`)
  while depending only on `adbc-driver-manager` + `pyarrow` and loading
  the bundled Go shared library. The 1.x integration suite (34 tests),
  OAuth unit tests, and SQL-routing unit tests run **verbatim** against
  the Go backend — all green — and now run in CI on linux and macos.
  `test_connect_unit.py` is adapted to the 2.0 seams (databases built
  via `adbc_driver_manager.AdbcDatabase` with the bundled Go driver)
  with the same test intents and the 1.x `TestIsDdlDml` cases verbatim.
- Platform wheels: `py3-none-<platform>` wheels embedding the shared
  library (`make -C go wheel`), built in CI for linux amd64/arm64
  (auditwheel-repaired to manylinux), macOS arm64/amd64
  (MACOSX_DEPLOYMENT_TARGET 11.0), and windows amd64, and attached to
  GitHub Releases alongside the standalone library tarballs.
