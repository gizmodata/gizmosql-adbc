# Downstream compatibility campaign (2026-07-29)

The 2.0 wheel was force-installed over the 1.x pin in every gizmodata
repo that depends on `adbc-driver-gizmosql`, and each repo's own test
suite was run in an isolated venv (fail attribution: 2.0-caused vs
pre-existing vs environmental, with 1.x baseline reruns where needed).

| Repo | Suite | 2.0 result | Notes |
|---|---|---|---|
| ibis-sqlflite (ibis-gizmosql) | full, `-x -n auto` | ✅ **1563 passed** (22 skip / 56 xfail, suite's own markers) | bulk ingest, dbgen, TZ handling |
| dbt-gizmosql | full functional suite | ✅ **141 passed** = 1.x baseline | after options-delegation fix (below); MinIO test env-broken on this machine under both drivers |
| sqlframe-gizmosql | unit + integration | ✅ 48 passed | |
| sqlmesh-gizmosql | unit + non-ducklake integration | ✅ 36 passed — **better than 1.x** here | after bind-reset fix (below); 1.x also fails ingest-then-reuse under flightsql 1.12; ducklake blocked by machine-local DuckDB stored-secret collision under both drivers |
| xorq (gizmosql backend) | backend tests + live smoke | ✅ 73 passed = 1.x baseline | incl. TLS; drove the pyarrow-floor relaxation |
| gizmosql-py | offline + network tiers | ✅ 17 passed | real-server ADBC round trip |
| generate-gizmosql-token | full (JWT auth, live server) | ✅ 22 passed = 1.x baseline | |
| qgizmosql | unit + live TLS integration | ✅ 33 passed = 1.x baseline | verified venv driver wins over stale bundled 1.1.5 |

**Total: ~1,933 downstream tests passing on the 2.0 driver.**

## Regressions found and fixed during the campaign

1. **Hidden option interfaces** (dbt blocker): wrapper embedding hid
   `adbc.GetSetOptions`/`PostInitOptions` from type assertions →
   `adbc.connection.catalog` failed. Fixed with explicit delegation on
   database/connection/statement + live regression test.
2. **Silent data loss after `adbc_ingest`** (sqlmesh blocker): sticky
   bound-data flag disabled DDL/DML routing on reused statements, and
   upstream 1.12 leaves bound data staged after a completed ingest
   (poisoning statement reuse). Fixed by transparent inner-statement
   recreation on the next `SetSqlQuery` with option replay + live
   regression test.
3. **pyarrow floor** (xorq resolver conflict): relaxed to `>=14.0.0`.
