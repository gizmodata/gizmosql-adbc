# Migrating from adbc-driver-gizmosql 1.x to 2.0

**TL;DR: `pip install --upgrade adbc-driver-gizmosql` — your code should
not need to change.**

2.0 keeps the 1.x Python API byte-compatible while moving the
GizmoSQL-specific behavior (DDL/DML immediate execution under the
lazy-execution model, `RETURNING` eager materialization, the
`gizmosql://` URI scheme) into a native Go driver library bundled inside
the wheel. The verbatim 1.x test suite is this repo's release gate.

## What stays the same

- `gizmosql.connect(...)` — every keyword argument (`uri`, `profile`,
  `username`, `password`, `tls_skip_verify`, `auth_type="external"`,
  `oauth_*`, `open_browser`, `db_kwargs`, `conn_kwargs`, `autocommit`)
- `cursor.execute()` semantics: DDL/DML executes immediately, no fetch
  required; `INSERT/UPDATE/DELETE ... RETURNING` rows are fetchable and
  always persist; `description` is `None` after DDL; `rowcount` is set
- `cursor.execute_update()` and module-level
  `dbapi.execute_update(cursor, query)`
- `get_oauth_token()`, `OAuthResult`, `GizmoSQLOAuthError` — the OAuth
  browser flow is unchanged (still pure stdlib, still client-side)
- `DatabaseOptions` / `ConnectionOptions` constants (now vendored)
- Connection profiles (`profile=`, `profile://<name>`, env-var
  substitution, option precedence)
- All URI schemes: `gizmosql://` (TLS by default), `grpc+tls://`,
  `grpc+tcp://`, `grpc://`, `flightsql://`

## What changes under the hood

- Dependencies: `adbc-driver-flightsql` is **no longer a dependency** —
  2.0 depends only on `adbc-driver-manager` and `pyarrow`, and bundles
  `libadbc_driver_gizmosql` (built from this repo's Go driver) in
  platform wheels (linux amd64/arm64 manylinux, macOS arm64/amd64,
  windows amd64).
- The `gizmosql://` → `flightsql://` rewrite, DDL/DML routing, and
  `RETURNING` materialization happen inside the driver library — so
  they now also work from Go, C/C++, R, C#, Rust, Ruby, and JavaScript,
  and via `driver = "gizmosql"` driver manifests and connection
  profiles in any language.

## Edge cases to be aware of

- Code that imported *private* internals of the 1.x package (e.g.
  `adbc_driver_gizmosql.dbapi._CachedRowIterator`) may find them gone;
  the private SQL-classification helpers (`_is_ddl_dml`,
  `_has_returning_clause`, `_strip_sql_comments`, `_extract_host`) are
  retained.
- If you need to point at a custom driver build, set
  `GIZMOSQL_DRIVER_LIB=/path/to/libadbc_driver_gizmosql.<ext>`.

## New capabilities you get for free

- Use the driver from any ADBC language via a
  [driver manifest](https://arrow.apache.org/adbc/current/format/driver_manifests.html)
  (`packaging/gizmosql.toml.in`) — `driver = "gizmosql"`.
- OAuth from non-Python languages via `adbc.gizmosql.*` database
  options (including profiles).
- Native Go: `import "github.com/gizmodata/gizmosql-adbc/go/gizmosql"`.
