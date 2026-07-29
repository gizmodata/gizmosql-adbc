# Columnar ADBC QuickStarts conformance

Acceptance gate 3 tracks the gizmosql examples in
[columnar-tech/adbc-quickstarts](https://github.com/columnar-tech/adbc-quickstarts)
(today written against the generic Flight SQL driver, loaded by manifest
name `flightsql`). Conformance means: the example body works unchanged
except `driver = "gizmosql"` (this driver's manifest) — gaining DDL/DML
immediacy, `RETURNING`, `gizmosql://` URIs, and OAuth in the process.

All nine language quickstarts resolve the driver through their ADBC
driver manager + the driver manifest — the exact mechanism this repo
ships (`packaging/gizmosql.toml.in`, entrypoint `AdbcDriverInit`).

## Status (2026-07-29, local macOS arm64, live GizmoSQL server)

| Quickstart | Path through driver | Result |
|---|---|---|
| python | `adbc_driver_manager.dbapi`, `driver="gizmosql"` via `ADBC_DRIVER_PATH` manifest | ✅ pass (`SELECT * FROM region` → 5 rows) |
| go | `arrow-adbc/go/adbc/drivermgr` (C driver manager), `"driver": "gizmosql"` | ✅ pass (5 rows) |
| cpp | C driver manager, manifest | ⏳ not yet run (needs cmake toolchain; same drivermgr+manifest path as the two passes above) |
| r | adbcdrivermanager, manifest | ⏳ not yet run |
| java | Java driver manager (JNI manifests since arrow-adbc 24) | ⏳ not yet run |
| rust | adbc_core driver manager, manifest | ⏳ not yet run |
| ruby | red-adbc, manifest | ⏳ not yet run |
| kotlin | as java | ⏳ not yet run |
| javascript | adbc JS driver manager, manifest | ⏳ not yet run |

The two verified languages cover both driver-manager implementations the
others delegate to (the C library and the Go drivermgr cgo bridge). The
remaining rows need their language toolchains installed; run them the
same way:

```bash
# 1. Build the driver and stage a manifest
make -C go lib
export ADBC_DRIVER_PATH=$PWD/scratch/drivers   # containing gizmosql.toml

# 2. Start the demo server (docker run ... gizmodata/gizmosql, TPC-H)

# 3. Run the quickstart with driver name "gizmosql" instead of "flightsql"
```
