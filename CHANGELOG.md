# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

Versioning note: this repo's releases continue the `adbc-driver-gizmosql`
line — the first release from this repo will be 2.0.0 (the Go driver plus
Python bindings), succeeding the 1.x pure-Python driver.

## [Unreleased]

### Changed
- Python dbapi: fetching after a successfully executed DDL/DML statement
  (`cursor.execute("CREATE ...")` then `fetchall()`) now returns an empty
  result (`None`/`[]`) instead of raising
  `ProgrammingError: Cannot fetchall() before execute()`, matching
  `sqlite3`/`duckdb` semantics. Generic DB-API consumers (e.g. sqlframe)
  fetch unconditionally after `execute()` and only then inspect
  `cursor.description`; the strict raising broke server-side
  `spark.read.*` schema inference in sqlframe-gizmosql. Fetching on a
  cursor that never executed still raises.

## [2.0.4] - 2026-07-29

### Fixed
- `packaging/gizmosql.toml.in`: the `windows_amd64` and `windows_arm64`
  shared-library paths pointed at `adbc_driver_gizmosql.dll`, but the
  release tarballs ship `libadbc_driver_gizmosql.dll` (verified against
  the v2.0.3 assets for both architectures). Manifest-based driver
  loading on Windows could not find the DLL as a result.

## [2.0.3] - 2026-07-29

### Fixed
- README Docker quickstart: added
  `--env DATABASE_FILENAME="data/TPC-H-small.duckdb"` — without it,
  recent `gizmodata/gizmosql` images open an in-memory database and the
  `nation` queries in the examples fail. Every example in the PyPI
  README was run end-to-end against the published 2.0.2 wheels as part
  of this fix.

## [2.0.2] - 2026-07-29

### Changed
- The PyPI package description is now a hand-maintained, Python-focused
  README (`python/README.md`) modeled on the 1.x driver's README —
  install, DBAPI usage, URI schemes, DDL/DML immediacy, OAuth/SSO,
  connection profiles, bulk ingest, observability, and API reference —
  instead of mirroring the repository README (which centers on the Go
  driver and multi-language usage). The `scripts/sync_pypi_readme.py`
  mirror step introduced in 2.0.1 is removed from the wheel builds.

## [2.0.1] - 2026-07-29

### Fixed
- **Geometry columns now ingest as `GEOMETRY`, not `BLOB`**
  ([adbc-driver-gizmosql#5](https://github.com/gizmodata/adbc-driver-gizmosql/issues/5)):
  when bound ingest data contains `geoarrow.*` Arrow extension columns
  (as produced by fetching GizmoSQL geometry columns), the driver now
  routes the bulk ingest through a session-temporary interim table,
  restores the geometry type via `ST_GeomFromWKB`, and materializes into
  the target per the requested mode. This fixes both create-family modes
  (previously produced `BLOB` columns) and append mode into existing
  `GEOMETRY` tables (previously failed server-side with a blob→geometry
  conversion error). Non-geometry ingests are unaffected (plain
  delegation). Covered by a live-server regression test across all four
  ingest modes with WKB round-trip value verification.

### Changed
- The PyPI package description now mirrors the repository README
  (relative links rewritten to absolute GitHub URLs), synced by
  `scripts/sync_pypi_readme.py` in every wheel build so the two cannot
  drift — matching the 1.x package's behavior.

### Added
- Go module publishing: `go/vX.Y.Z` tags version the
  `github.com/gizmodata/gizmosql-adbc/go` module (its own v1 line,
  independent of the Python `v*` tags), and a `go-module.yml` workflow
  publishes the version through proxy.golang.org and warms the
  pkg.go.dev docs cache on tag push (adbc-driver-quack pattern). First
  Go module release: `go/v1.0.0`.

## [2.0.0] - 2026-07-29

### Added (release packaging)
- Windows arm64 builds: the shared library and platform wheel are built
  on GitHub's native `windows-11-arm` runners and attached to releases
  (manifest template gains a `windows_arm64` entry) — six platforms
  total.
- CI proof that the `py3-none-<platform>` wheels support every CPython
  from 3.10 through 3.14: a per-version job installs the wheel and runs
  the full test suite on each.

### Fixed
- **Option interfaces restored through the wrappers** (found by running
  the dbt-gizmosql suite against 2.0): the database/connection/statement
  wrappers now delegate the full `adbc.GetSetOptions` /
  `adbc.PostInitOptions` surface to the upstream driver, so
  `adbc.connection.catalog` (dbt's database credential) and friends work
  again pre- and post-init, and `adbc_current_catalog` reads back.
  Live-server regression test added.
- **Silent data loss after bulk ingest** (found by running the
  sqlmesh-gizmosql suite): `Bind`/`BindStream` left a sticky
  bound-data flag that permanently disabled DDL/DML routing on a reused
  statement — and upstream arrow-adbc 1.12 additionally leaves the bound
  stream staged after a completed ingest and then rejects any later
  plain-SQL execution on that statement. Python's dbapi cursor reuses
  one ADBC statement for its lifetime, so after `cursor.adbc_ingest()`
  every subsequent `INSERT`/`COMMIT` on that cursor was silently
  no-oped. The statement wrapper now transparently recreates the inner
  statement on the next `SetSqlQuery`/`SetSubstraitPlan` when stale
  bound data exists, replaying any recorded statement options.
  Live-server regression test (`TestIntegrationDDLDMLRoutingAfterBind`)
  reproduces the ingest-then-DML flow.

### Changed
- Release workflow now publishes the five platform wheels to PyPI via
  OIDC trusted publishing on `v*` tag pushes (`pypi-publish` job) —
  requires the `gizmosql-adbc` repo + `release.yml` workflow to be
  registered as a trusted publisher on the `adbc-driver-gizmosql` PyPI
  project.
- Relaxed the Python bindings' `pyarrow` floor from `>=25.0.0` to
  `>=14.0.0` — the bindings only touch pyarrow via adbc-driver-manager,
  and the strict floor conflicted with downstream consumers (e.g. xorq
  pins `pyarrow<22`) that otherwise run fine.

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
- Conformance + docs (Phase 6, first slice): the Columnar
  adbc-quickstarts gizmosql examples pass for python and go with
  `driver = "gizmosql"` resolved through the shipped driver manifest
  (status table in `docs/quickstarts-conformance.md`), and a 1.x → 2.0
  migration guide (`docs/migrating-1x-to-2.md`) documents the
  byte-compatible API and the under-the-hood changes.
