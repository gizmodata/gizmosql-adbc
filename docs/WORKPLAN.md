# Work plan — gizmosql-adbc build-out

Living checklist for the (partly autonomous) build-out toward
`adbc-driver-gizmosql` 2.0. Worked in order; each completed item is checked
off, committed, and pushed with green tests. See `docs/plan.md` for design.

## Ground rules (for autonomous iterations)

- Every commit must build (`go build ./...`) and pass tests (`go test ./...`,
  and `pytest` once `python/` exists). No red pushes to `main`.
- Behavior parity is defined by the 1.x test suite in
  `gizmodata/adbc-driver-gizmosql` — port tests, don't reinvent them.
- Pin `github.com/apache/arrow-adbc/go/adbc` to a released version; note
  upgrades in CHANGELOG.
- Update `CHANGELOG.md` (`[Unreleased]`) with every functional change.
- **Do not tag or publish any release** — Philip cuts releases.
- Integration tests run against a real GizmoSQL server (the `gizmosql` PyPI
  package's managed subprocess, as in 1.x `tests/conftest.py`, or Docker).
- When blocked (credentials, design fork, upstream bug), record the blocker
  at the bottom of this file and move to the next unblocked item.

## Acceptance gates for 2.0 (Philip, 2026-07-29)

1. The **full 1.x pytest suite, verbatim** (copied from
   `gizmodata/adbc-driver-gizmosql`, not rewritten) passes against the
   2.0 Python bindings backed by the Go driver.
2. Native **Go tests** cover the driver internals (unit + live-server
   integration).
3. Every **gizmosql example in the Columnar ADBC QuickStarts repo**
   (https://github.com/columnar-tech/adbc-quickstarts — e.g.
   `cpp/flightsql/gizmosql`, and the other languages' gizmosql examples,
   which today use the generic Flight SQL Go driver) works when pointed
   at this driver's shared library.
4. GizmoSQL's **lazy-execution model** is handled inside the driver
   (DDL/DML immediate execution, RETURNING) so ALL consumers get correct
   behavior without client-side workarounds.

## Phase 1 — Go pass-through driver

- [x] `go/` module `github.com/gizmodata/gizmosql-adbc/go` (Go 1.26),
      pinned to `arrow-adbc/go/adbc` v1.12.0
- [x] `go/gizmosql` package: `NewDriver(alloc)` delegating 100% to the
      upstream flightsql driver (adbc.Driver / Database / Connection /
      Statement wrappers in place, no behavior change yet)
- [x] `gizmosql://` URI option handling: rewrite to `flightsql://`
      (TLS default, `?transport=tcp`), unit tests (recording-fake
      downstream assertions + real-driver option validation)
- [x] Go integration harness: launches the cached `gizmosql_server`
      binary (GIZMOSQL_SERVER_BIN / PATH / pip-cache discovery) with a
      Go-minted self-signed TLS cert; `SELECT 1` over `gizmosql://` and
      legacy-scheme parity; skipped under `-short` or when no binary
- [x] GitHub Actions CI: gofmt/vet/unit (`-short`) + live-server
      integration tests on ubuntu + macos (server fetched via
      `pip install gizmosql` + `ensure_binary`)

## Phase 2 — DDL/DML + RETURNING routing

- [x] Port `_is_ddl_dml` semantics: keyword set, block/line comment
      stripping, string-literal-aware `RETURNING` detection — as Go table
      tests mirroring 1.x `test_sql_routing_unit.py` cases (sqlrouting.go)
- [x] Statement wrapper: `ExecuteQuery` on DDL/DML → `ExecuteUpdate`
      (DoPut) under the hood; empty-schema result + affected count
      (plain SQL only — Bind/SetSubstraitPlan disable routing, matching
      1.x `parameters is None` semantics)
- [x] `RETURNING` → query path with **eager materialization** (DML fires
      even if the caller never reads the stream); live-server tests for
      persist-without-fetch, returned rows, and string-literal
      'returning' non-misroute
- [x] Thread-safe GetInfo (mutex-serialized on the connection wrapper —
      guards apache/arrow-adbc#1178 concurrent-map crash; 1.x parity)

## Phase 3 — OAuth/SSO

- [x] Port `_oauth.py` flow to Go stdlib (`net/http`): initiate → browser
      → poll token endpoint; TLS-skip-verify + HTTPS→HTTP fallback
      discovery; context-cancellable (oauth.go, `GetOAuthToken`)
- [x] Driver options: `adbc.gizmosql.auth_type=external`,
      `adbc.gizmosql.oauth.url/.port/.timeout_seconds/
      .poll_interval_seconds/.open_browser/.tls_skip_verify` (OAuth TLS
      verify defaults to the Flight SQL client's tls_skip_verify);
      options consumed before delegation so the upstream driver never
      sees unknown keys; token injected as username="token" Basic Auth
- [x] Headless mode: `open_browser=false` prints the auth URL to stderr;
      Go-native callers can set `OAuthConfig.AuthURLHandler` for full
      control
- [x] Unit tests with mock OAuth HTTP servers (mirror 1.x
      `test_oauth_unit.py`): success-after-pending, error/not_found/
      unexpected status, missing token, timeout, malformed initiate,
      HTTP discovery fallback, driver-level token injection and option
      validation

## Phase 4 — C shared library + release matrix

- [ ] cgo exports producing `libadbc_driver_gizmosql.{so,dylib,dll}`
      (modeled on upstream `go/adbc/pkg` templates)
- [ ] Driver manifest so `driver = "gizmosql"` resolves via driver managers
- [ ] CI build matrix: manylinux amd64/arm64, macOS arm64/x86_64,
      windows amd64; binaries attached to GitHub Releases on tag push
      (CHANGELOG-extracted release notes, per house release pattern)

## Phase 5 — Python bindings (adbc-driver-gizmosql 2.0)

- [ ] `python/` package: same public API as 1.x (`dbapi.connect()`,
      `get_oauth_token`, `execute_update`), loading the bundled shared
      library via `adbc-driver-manager`
- [ ] Port the full 1.x pytest suite; all green against the Go backend
- [ ] Per-platform wheels embedding the shared library; sdist story
- [ ] PyPI trusted publishing from this repo — **requires Philip**: add
      this repo + its publish workflow as a trusted publisher on the
      existing `adbc-driver-gizmosql` PyPI project (publishers are bound
      to repo+workflow, so the 1.x repo's grant does not carry over)

## Phase 6 — Other bindings & docs

- [ ] R / C# / Rust / JS usage docs via driver manager + manifest
- [ ] Migration guide 1.x → 2.0
- [ ] Sunset the 1.x repo (after a post-2.0 maintenance window, decided
      by Philip): pointer README, close/redirect open issues, then
      GitHub-archive `gizmodata/adbc-driver-gizmosql` (read-only, history
      and links preserved)

## Blockers / notes

- The Database/Connection/Statement wrappers embed the upstream
  interfaces, which hides upstream *optional* interfaces
  (`adbc.GetSetOptions`, `adbc.DatabaseLogging`, statistics, ...) from
  type assertions. Before the cgo export phase (Phase 4), add explicit
  delegation for the optional interfaces the C driver-manager layer
  probes for. (Noted 2026-07-29 during Phase 1.)
