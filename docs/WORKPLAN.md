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
- [ ] Go integration harness: spin up GizmoSQL server (subprocess or
      Docker) with self-signed TLS; smoke test `SELECT 1`
- [ ] GitHub Actions CI: gofmt/vet/build/test on linux + macos

## Phase 2 — DDL/DML + RETURNING routing

- [ ] Port `_is_ddl_dml` semantics: keyword set, block/line comment
      stripping, string-literal-aware `RETURNING` detection — as Go table
      tests mirroring 1.x `test_sql_routing_unit.py` cases
- [ ] Statement wrapper: `ExecuteQuery` on DDL/DML → `ExecuteUpdate`
      (DoPut) under the hood; empty schema result semantics
- [ ] `RETURNING` → query path with **eager materialization** (DML fires
      even if the caller never reads the stream); port the 1.x
      `TestReturningClause` integration tests
- [ ] Thread-safe GetInfo caching (1.x `adbc_get_info` fix parity)

## Phase 3 — OAuth/SSO

- [ ] Port `_oauth.py` flow to Go stdlib (`net/http`): initiate → browser
      → poll token endpoint; TLS-skip-verify + HTTP fallback discovery
- [ ] Driver options: `adbc.gizmosql.auth_type=external`,
      `adbc.gizmosql.oauth.url/.port/.timeout_seconds/.open_browser`
- [ ] Headless mode: expose auth URL + poll via options/callback instead
      of opening a browser
- [ ] Unit tests with a mock OAuth HTTP server (mirror 1.x
      `test_oauth_unit.py`)

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
