# Claude Code Guidelines for gizmosql-adbc

## Release checklist

Releases are CI-driven: update `CHANGELOG.md` (move `[Unreleased]` into a
dated `## [X.Y.Z]` section), bump `python/pyproject.toml` and
`python/src/adbc_driver_gizmosql/_version.py`, commit, then
`git push origin main vX.Y.Z`. The `release.yml` workflow builds the six
platform shared libraries, publishes the wheels to PyPI via trusted
publishing, and creates the GitHub release with the CHANGELOG section as
notes. Confirm with `gh run watch` and `pip index versions adbc-driver-gizmosql`.

## After every driver release: bump the dependents, recursively

Several repos pin an exact driver version and ship the driver inside their
own artifacts. Users of those wrappers do not get a driver fix until the pin
moves, so a driver release is not finished until these are bumped and
released too, in this order (each follows the same CHANGELOG + version bump +
`vX.Y.Z` tag pattern; CI publishes):

1. **`@gizmodata/gizmosql-client`** — repo checkout at
   `~/LocalOnly/git/flight-sql-client-js` (the `gizmosql-client-js` directory
   is a stale duplicate of the same remote; do not use it).
   `node scripts/pin-driver.mjs <version>` downloads the release assets and
   rewrites `driver-manifest.json` with their SHA-256s. Then
   `npm version X.Y.Z --no-git-tag-version`, CHANGELOG, `npm run typecheck`,
   `npm test`, commit, tag. Wait until `npm view @gizmodata/gizmosql-client
   version` shows the new version before step 2.
2. **`@gizmodata/gizmosql-mcp`** — `~/LocalOnly/git/gizmosql-mcp`.
   `npm install @gizmodata/gizmosql-client@^<new>` and update the
   `allowScripts` key `"@gizmodata/gizmosql-client@<exact>"` in package.json.
   The release check requires package.json, `charts/gizmosql-mcp/Chart.yaml`
   (`version` and `appVersion`) and `manifest.json` (MCPB bundle) to all
   equal the tag — bump all three. `npm run typecheck && npm run lint:ci &&
   npm test`, commit, tag.
3. **gizmosql-ui** — `~/LocalOnly/git/gizmosql-ui`. Depends on the client
   with a `^` range, but the packaged app ships whatever `package-lock.json`
   resolved, so run `npm install @gizmodata/gizmosql-client@^<new>`, add a
   CHANGELOG section (no `[Unreleased]` header in that file — insert a dated
   section at the top), `npm version X.Y.Z --no-git-tag-version`,
   `npm run lint && npm run build`, commit, tag `vX.Y.Z`.
4. **gizmosql server `-adbc` images** — `~/LocalOnly/git/gizmosql`
   `Dockerfile-adbc.ci` `ARG GIZMOSQL_ADBC_VERSION="vX.Y.Z"` plus a CHANGELOG
   entry; ships with the next server release.
5. **qgizmosql (QGIS plugin)** — `~/LocalOnly/git/qgizmosql`
   `qgizmosql/requirements.txt` pins `adbc-driver-gizmosql==X.Y.Z` (embedded
   into the plugin ZIP at build time). Bump it, add a CHANGELOG section and a
   `changelog=` line in `qgizmosql/metadata.txt`, run
   `python3.12 -m flake8 qgizmosql` and `python3.12 -m unittest discover -s
   tests/unit`, commit, tag.

Python consumers (dbt-gizmosql, sqlmesh-gizmosql, ibis-gizmosql, gizmosql-py,
gizmodata-nexus-api, ...) use `>=` ranges and need nothing.

Before assuming the list is complete, re-grep for hard pins:

```
grep -rIn --exclude-dir={.git,node_modules,build,dist,.venv} -E \
  "gizmosql-adbc|adbc-driver-gizmosql|GIZMOSQL_ADBC_VERSION|libadbc_driver_gizmosql" \
  ~/LocalOnly/git | grep -E "[0-9]+\.[0-9]+\.[0-9]+"
```

Gate every tag on the exit codes of the test commands in the same shell
chain. A chain gated on a `grep` of test output once matched nothing, skipped
the commit, and tagged the previous release commit.

## Execution routing (why the driver exists)

GizmoSQL executes query-path statements lazily on DoGet. The statement
wrapper in `go/gizmosql/driver.go` routes DDL/DML — with or without bound
parameters — through `ExecuteUpdate` (DoPut) so it executes immediately, and
materializes `RETURNING` results. Keep that invariant when touching
`ExecuteQuery`; the live-server tests in `routing_integration_test.go` and
the Python `TestExecuteAutoDetect` / `TestBoundParameterRouting` suites
guard it. Run them with `GIZMOSQL_SERVER_BIN=<path to gizmosql_server>`.
