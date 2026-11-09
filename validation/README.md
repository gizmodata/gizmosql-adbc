# ADBC conformance (validation) suite

This directory runs the [adbc-drivers.org validation suite](https://github.com/adbc-drivers/validation)
against this driver and a live GizmoSQL server. The suite is a pinned test dependency
(`pyproject.toml`, `uv.lock`); the files here are the GizmoSQL glue: driver quirks
(`tests/gizmosql.py`), DuckDB-specific expected results (`queries/`), and known server bugs
(`tests/vendor_bugs.py`, plus `broken-vendor` tags in `queries/`).

CI runs it as the `adbc validation suite` job of `.github/workflows/ci.yml`, which is part of the
release gate.

## Run it locally

```bash
make -C go lib
docker run -d --name gizmosql-adbc-validation -p 127.0.0.1:31447:31337 \
  -e GIZMOSQL_USERNAME=gizmosql_user -e GIZMOSQL_PASSWORD=gizmosql_password \
  gizmodata/gizmosql:v1.42.2-slim
cd validation && uv run pytest tests -rfEsxX
```

Settings (defaults in parentheses) come only from project-prefixed variables:
`GIZMOSQL_ADBC_TEST_URI` (`grpc://127.0.0.1:31447`), `GIZMOSQL_ADBC_TEST_USERNAME`
(`gizmosql_user`), `GIZMOSQL_ADBC_TEST_PASSWORD` (`gizmosql_password`) and
`GIZMOSQL_ADBC_TEST_DRIVER_PATH` (`go/build/libadbc_driver_gizmosql.<ext>`).

The run fails (exit 3) when the driver library is missing or the server is unreachable, and
fails when no test passed; it never turns into a run of skips.

## Unsupported features and known server bugs

Features the driver does not offer are `False` in `tests/gizmosql.py`, with the reason.
Each known GizmoSQL server bug is an xfail that first checks the test failed with that bug's
specific error, so a different failure, or a fixed server, turns the run red. The xfails name
GizmoSQL v1.42.2, the server version CI pins; upgrade the pinned image deliberately and
remove the entries the new server fixes.
