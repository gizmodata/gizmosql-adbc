"""Server-side query cancellation (CancelFlightInfo) through the Python
DB-API layer, against a live GizmoSQL server.

The server's ``--print-queries`` log is the oracle: ``gizmosql_server`` logs
``SQL Statement was successfully canceled.`` when a CancelFlightInfo action
interrupts the session's active statement.
"""

from __future__ import annotations

import os
import signal
import subprocess
import sys
import textwrap
import threading
import time
from pathlib import Path

import pytest

pytestmark = pytest.mark.integration

# CPU-bound for far longer than any test timeout: the only way it returns
# promptly is a server-side interrupt.
LONG_QUERY = "SELECT sum(a.range * b.range) FROM range(100000000) a, range(100000) b"

ATTEMPT_MARKER = "status=attempt"
CANCELED_MARKER = "SQL Statement was successfully canceled"


def _count(log: Path, marker: str) -> int:
    return log.read_text(errors="replace").count(marker) if log.exists() else 0


def _wait_for_count(log: Path, marker: str, at_least: int, timeout: float = 20.0) -> int:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        n = _count(log=log, marker=marker)
        if n >= at_least:
            return n
        time.sleep(0.05)
    pytest.fail(f"server log never reached {at_least} x {marker!r} within {timeout}s")


@pytest.fixture()
def fresh_conn(gizmosql_uri):
    """A dedicated connection per test so each test owns its own server
    session (cancels are session-scoped)."""
    from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

    from adbc_driver_gizmosql import dbapi as gizmosql

    with gizmosql.connect(
        gizmosql_uri,
        username=GIZMOSQL_USERNAME,
        password=GIZMOSQL_PASSWORD,
        tls_skip_verify=True,
    ) as connection:
        yield connection


def _assert_session_usable(conn) -> None:
    with conn.cursor() as cur:
        cur.execute("SELECT 42")
        assert cur.fetchone()[0] == 42


class TestQueryCancellation:
    def test_adbc_cancel_from_another_thread_interrupts_query(
        self, fresh_conn, gizmosql_server_log
    ):
        """cursor.adbc_cancel() — the same entry point the driver manager's
        SIGINT handler uses — interrupts a running query on the server."""
        attempts_before = _count(log=gizmosql_server_log, marker=ATTEMPT_MARKER)
        cancels_before = _count(log=gizmosql_server_log, marker=CANCELED_MARKER)
        cur = fresh_conn.cursor()
        errors: list[BaseException] = []

        def run() -> None:
            try:
                cur.execute(LONG_QUERY)
                cur.fetchall()
            except BaseException as exc:  # noqa: BLE001 - we want whatever the driver raised
                errors.append(exc)

        worker = threading.Thread(target=run, daemon=True)
        started = time.monotonic()
        worker.start()
        _wait_for_count(
            log=gizmosql_server_log, marker=ATTEMPT_MARKER, at_least=attempts_before + 1
        )
        time.sleep(1.0)

        cur.adbc_cancel()
        worker.join(timeout=30)

        assert not worker.is_alive(), "query was not interrupted within 30s of adbc_cancel()"
        assert errors, "expected the long query to fail"
        # The client sees whichever lands first: the server's interrupt or
        # the local context cancel the driver issues right after it.
        msg = str(errors[0]).upper()
        assert "INTERRUPT" in msg or "CANCEL" in msg, msg
        assert time.monotonic() - started < 30
        _wait_for_count(
            log=gizmosql_server_log, marker=CANCELED_MARKER, at_least=cancels_before + 1, timeout=5
        )
        cur.close()
        _assert_session_usable(conn=fresh_conn)

    def test_cursor_close_cancels_running_query(self, fresh_conn, gizmosql_server_log):
        """GizmoSQL returns the schema in the FlightInfo, so execute()
        returns before the query has run; closing the cursor while the
        server is still executing must interrupt it."""
        attempts_before = _count(log=gizmosql_server_log, marker=ATTEMPT_MARKER)
        cancels_before = _count(log=gizmosql_server_log, marker=CANCELED_MARKER)
        cur = fresh_conn.cursor()
        cur.execute(LONG_QUERY)
        _wait_for_count(
            log=gizmosql_server_log, marker=ATTEMPT_MARKER, at_least=attempts_before + 1
        )
        time.sleep(1.0)

        cur.close()  # walk away without fetching

        _wait_for_count(
            log=gizmosql_server_log, marker=CANCELED_MARKER, at_least=cancels_before + 1, timeout=5
        )
        _assert_session_usable(conn=fresh_conn)

    def test_fully_fetched_result_is_not_cancelled(self, fresh_conn, gizmosql_server_log):
        """Draining a result and closing the cursor sends no cancel."""
        cancels_before = _count(log=gizmosql_server_log, marker=CANCELED_MARKER)
        with fresh_conn.cursor() as cur:
            cur.execute("SELECT range AS v FROM range(1000)")
            rows = cur.fetchall()
        assert len(rows) == 1000
        time.sleep(0.5)
        assert _count(log=gizmosql_server_log, marker=CANCELED_MARKER) == cancels_before

    def test_ctrl_c_in_client_process_cancels_query_on_server(
        self, gizmosql_uri, gizmosql_server_log, tmp_path
    ):
        """The reported scenario: a user interrupts their Python process
        mid-query. SIGINT reaches the driver manager's handler, which calls
        AdbcStatementCancel; the driver must relay that to the server."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        attempts_before = _count(log=gizmosql_server_log, marker=ATTEMPT_MARKER)
        cancels_before = _count(log=gizmosql_server_log, marker=CANCELED_MARKER)

        script = tmp_path / "client.py"
        script.write_text(
            textwrap.dedent(
                f"""
                import sys
                from adbc_driver_gizmosql import dbapi as gizmosql

                with gizmosql.connect(
                    {gizmosql_uri!r},
                    username={GIZMOSQL_USERNAME!r},
                    password={GIZMOSQL_PASSWORD!r},
                    tls_skip_verify=True,
                ) as conn, conn.cursor() as cur:
                    print("ready", flush=True)
                    cur.execute({LONG_QUERY!r})
                    cur.fetchall()
                print("finished", flush=True)
                """
            )
        )
        proc = subprocess.Popen(
            [sys.executable, str(script)],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env={**os.environ, "PYTHONUNBUFFERED": "1"},
        )
        try:
            _wait_for_count(
                log=gizmosql_server_log,
                marker=ATTEMPT_MARKER,
                at_least=attempts_before + 1,
                timeout=60,
            )
            time.sleep(1.0)
            proc.send_signal(signal.SIGINT)
            try:
                stdout, stderr = proc.communicate(timeout=30)
            except subprocess.TimeoutExpired:
                proc.kill()
                pytest.fail("client process did not exit within 30s of SIGINT")
        finally:
            if proc.poll() is None:
                proc.kill()

        assert "ready" in stdout
        assert "finished" not in stdout, "the long query should not have completed"
        assert proc.returncode != 0, f"expected KeyboardInterrupt exit, got 0; stderr={stderr}"
        _wait_for_count(
            log=gizmosql_server_log, marker=CANCELED_MARKER, at_least=cancels_before + 1, timeout=5
        )
