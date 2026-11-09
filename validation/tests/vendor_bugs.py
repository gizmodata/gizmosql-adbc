# Licensed under the Apache License, Version 2.0.

"""Known GizmoSQL server bugs the validation suite trips over.

Each entry is a server-side defect (or a deliberate server limitation), not a
driver bug: the driver passes the server's answer through unchanged.  The
overriding test runs the real suite test and requires it to fail with the
specific error recorded here, then marks it xfail.  If the test fails for a
different reason, or passes because the server was fixed, the run goes red,
so a stale entry can't hide anything.
"""

import re
import typing

import pytest

# The server reports every DuckDB error as gRPC InvalidArgument, which the
# Flight SQL driver maps (correctly) to ADBC INVALID_ARGUMENT; the ADBC spec
# wants NOT_FOUND / ALREADY_EXISTS for these cases.
WRONG_STATUS_NOT_FOUND = (
    "GizmoSQL v1.42.2 answers SetSessionOptions for a nonexistent catalog/schema "
    "with gRPC InvalidArgument instead of NotFound"
)
WRONG_STATUS_ALREADY_EXISTS = (
    "GizmoSQL v1.42.2 answers a bulk ingest that conflicts with an existing table "
    "(create on an existing table, append with a different schema) with gRPC "
    "InvalidArgument instead of AlreadyExists"
)
INGEST_UNQUALIFIED_TARGET = (
    "GizmoSQL v1.42.2 bulk ingest without a catalog/schema inserts into the "
    "unqualified table name, which DuckDB resolves to a same-named temporary "
    "table first, so a non-temporary ingest lands in the temporary table"
)
QUERY_PARAMETER_ROWS = (
    "GizmoSQL v1.42.2 refuses a query (SELECT) with more than one row of bind "
    "parameters; only prepared updates execute once per row"
)


def expect_vendor_bug(
    run: typing.Callable[[], None],
    *,
    reason: str,
    raises: type[BaseException] | tuple[type[BaseException], ...],
    match: str,
) -> None:
    """Run a suite test that must fail because of a known server bug."""
    with pytest.raises(expected_exception=raises) as excinfo:
        run()
    if not re.search(pattern=match, string=str(excinfo.value), flags=re.DOTALL):
        raise AssertionError(
            f"expected the known server bug ({reason}), but the test failed "
            f"differently: {excinfo.value}"
        ) from excinfo.value
    pytest.xfail(reason=reason)
