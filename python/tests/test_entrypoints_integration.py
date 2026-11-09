"""The library loads through every entrypoint it exports, over the C ABI.

The ADBC driver manager derives ``AdbcDriverGizmosqlInit`` from the file name
(``libadbc_driver_gizmosql``) and falls back to ``AdbcDriverInit``; those are the
supported entrypoints. ``GizmoSQLDriverInit`` and ``GizmoSqlDriverInit`` are
deprecated aliases kept exported until 3.0 so that existing configurations naming
them keep working (2.0.17 stopped exporting every other non-``Adbc`` symbol, per
the ADBC shared-library requirements).
"""

from __future__ import annotations

import ctypes

import adbc_driver_manager.dbapi
import pytest

from adbc_driver_gizmosql._driver import _driver_path

pytestmark = pytest.mark.integration


@pytest.mark.parametrize(
    "entrypoint",
    [
        None,  # the driver manager's derivation: AdbcDriverGizmosqlInit
        "AdbcDriverGizmosqlInit",
        "AdbcDriverInit",
        "AdbcDriverGizmoSQLInit",
        "GizmoSQLDriverInit",  # deprecated alias, removed in 3.0
        "GizmoSqlDriverInit",  # deprecated alias, removed in 3.0
    ],
)
def test_loads_and_queries_through_entrypoint(gizmosql_server, gizmosql_uri, entrypoint):
    from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

    with adbc_driver_manager.dbapi.connect(
        driver=_driver_path(),
        entrypoint=entrypoint,
        db_kwargs={
            "uri": gizmosql_uri,
            "username": GIZMOSQL_USERNAME,
            "password": GIZMOSQL_PASSWORD,
            "adbc.flight.sql.client_option.tls_skip_verify": "true",
        },
    ) as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT 42 AS answer")
            assert cur.fetchone() == (42,)


@pytest.mark.parametrize(
    "symbol", ["GizmoSQLDatabaseNew", "GizmoSQLStatementExecuteQuery", "_cgo_panic", "crosscall2"]
)
def test_internal_symbols_are_not_exported(symbol):
    """Go's //export functions and cgo glue are not exported (2.0.16 exported ~70-260 of them)."""
    lib = ctypes.CDLL(_driver_path())
    assert hasattr(lib, "AdbcDriverInit")
    assert not hasattr(lib, symbol)
