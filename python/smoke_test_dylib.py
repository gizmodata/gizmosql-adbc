"""Smoke-test the Go-built libadbc_driver_gizmosql through Python's ADBC
driver manager — the same C-ABI path every non-Go language uses.

Exercises the three behaviors the Go driver exists for:
  1. the gizmosql:// URI scheme,
  2. DDL/DML immediate execution despite GizmoSQL's lazy planning,
  3. INSERT ... RETURNING persisting even when never fetched.

Usage:
    pip install adbc-driver-manager gizmosql
    GIZMOSQL_DRIVER_LIB=/path/to/libadbc_driver_gizmosql.{so,dylib,dll} \
        python python/smoke_test_dylib.py
"""

from __future__ import annotations

import os
import pathlib
import sys

import adbc_driver_manager.dbapi as dbapi
import gizmosql


def default_lib() -> str:
    build = pathlib.Path(__file__).resolve().parent.parent / "go" / "build"
    for suffix in ("dylib", "so", "dll"):
        candidate = build / f"libadbc_driver_gizmosql.{suffix}"
        if candidate.exists():
            return str(candidate)
    sys.exit(
        "libadbc_driver_gizmosql not found — build it with `make -C go lib` "
        "or set GIZMOSQL_DRIVER_LIB"
    )


def main() -> None:
    lib = os.environ.get("GIZMOSQL_DRIVER_LIB") or default_lib()
    print(f"driver library: {lib}")

    with gizmosql.Server(username="u", password="p") as srv:
        uri = f"gizmosql://{srv.host}:{srv.port}?transport=tcp"
        with dbapi.connect(
            driver=lib,
            db_kwargs={"uri": uri, "username": "u", "password": "p"},
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1 AS v")
                assert cur.fetchone()[0] == 1
                print("PASS: SELECT over gizmosql:// via C ABI")

                cur.execute("CREATE TABLE t (id INT)")
                cur.execute("INSERT INTO t VALUES (1), (2)")
                assert cur.rowcount == 2, f"rowcount={cur.rowcount}, want 2"
                cur.execute("SELECT COUNT(*) FROM t")
                n = cur.fetchone()[0]
                assert n == 2, f"lazy-execution bug: COUNT(*)={n}, want 2"
                print("PASS: DDL/DML executes immediately (no fetch needed)")

                cur.execute("INSERT INTO t VALUES (3) RETURNING id")
                # Deliberately never fetch; the next query proves persistence.
                cur.execute("SELECT COUNT(*) FROM t")
                n = cur.fetchone()[0]
                assert n == 3, f"RETURNING not persisted: COUNT(*)={n}, want 3"
                print("PASS: INSERT..RETURNING persists without fetch")

    print("ALL PASS")


if __name__ == "__main__":
    main()
