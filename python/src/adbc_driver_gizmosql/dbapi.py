"""DBAPI 2.0 interface for GizmoSQL, backed by the native Go driver.

This module keeps the 1.x ``adbc_driver_gizmosql.dbapi`` API
byte-compatible while delegating to the bundled
``libadbc_driver_gizmosql`` shared library (built from this repo's Go
driver). GizmoSQL-specific execution semantics — the ``gizmosql://``
URI scheme, DDL/DML auto-detection with immediate execution, and
``RETURNING`` eager materialization — now live inside the driver
library, so every ADBC language shares one implementation.

Example (password auth)::

    from adbc_driver_gizmosql import dbapi as gizmosql

    with gizmosql.connect("gizmosql://localhost:31337",
                          username="user", password="pass",
                          tls_skip_verify=True) as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT 1")
            print(cur.fetch_arrow_table())
"""

from __future__ import annotations

import re as _re
import threading
from typing import Any, Dict, Optional, Union

import adbc_driver_manager
from adbc_driver_manager.dbapi import (
    Connection as _BaseConnection,
)
from adbc_driver_manager.dbapi import (
    Cursor as _BaseCursor,
)

# Re-export all DBAPI 2.0 symbols from the driver manager
from adbc_driver_manager.dbapi import (  # noqa: F401
    Date,
    DateFromTicks,
    Time,
    TimeFromTicks,
    Timestamp,
    TimestampFromTicks,
    apilevel,
    paramstyle,
    threadsafety,
)

from ._driver import _driver_path
from ._oauth import DEFAULT_OAUTH_PORT, get_oauth_token
from ._options import DatabaseOptions

# ---------------------------------------------------------------------------
# SQL classification helpers.
#
# The authoritative routing implementation now lives in the Go driver
# (go/gizmosql/sqlrouting.go) — these Python copies are retained for
# 1.x API compatibility (they were importable and are exercised by the
# 1.x unit-test suite, which this repo runs verbatim as its parity
# gate).
# ---------------------------------------------------------------------------

_DDL_DML_KEYWORDS = frozenset(
    {
        "ALTER",
        "ATTACH",
        "BEGIN",
        "CALL",
        "CHECKPOINT",
        "COMMENT",
        "COMMIT",
        "COPY",
        "CREATE",
        "DELETE",
        "DETACH",
        "DROP",
        "EXPORT",
        "GRANT",
        "IMPORT",
        "INSERT",
        "INSTALL",
        "LOAD",
        "MERGE",
        "REVOKE",
        "ROLLBACK",
        "SET",
        "TRUNCATE",
        "UPDATE",
        "USE",
        "VACUUM",
    }
)

_BLOCK_COMMENT_RE = _re.compile(pattern=r"/\*.*?\*/", flags=_re.DOTALL)
_LINE_COMMENT_RE = _re.compile(pattern=r"--[^\n]*")
_SINGLE_QUOTED_RE = _re.compile(pattern=r"'(?:[^']|'')*'")
_DOUBLE_QUOTED_RE = _re.compile(pattern=r'"(?:[^"]|"")*"')
_RETURNING_RE = _re.compile(pattern=r"\bRETURNING\b", flags=_re.IGNORECASE)


def _strip_sql_comments(sql: str) -> str:
    """Strip SQL block (/* ... */) and line (-- ...) comments."""
    sql = _BLOCK_COMMENT_RE.sub("", sql)
    sql = _LINE_COMMENT_RE.sub("", sql)
    return sql.lstrip()


def _has_returning_clause(sql: str) -> bool:
    """Return True if the SQL contains a ``RETURNING`` keyword outside of
    string literals. Comments must already be stripped by the caller."""
    without_strings = _SINGLE_QUOTED_RE.sub(repl="''", string=sql)
    without_strings = _DOUBLE_QUOTED_RE.sub(repl='""', string=without_strings)
    return _RETURNING_RE.search(string=without_strings) is not None


def _is_ddl_dml(operation) -> bool:
    """Return True if the SQL statement is DDL/DML based on the first keyword."""
    if not isinstance(operation, str):
        return False
    stripped = _strip_sql_comments(operation)
    if not stripped:
        return False
    first_word = stripped.split(maxsplit=1)[0].rstrip("(;")
    if first_word.upper() not in _DDL_DML_KEYWORDS:
        return False
    if _has_returning_clause(stripped):
        return False
    return True


class Cursor(_BaseCursor):
    """GizmoSQL cursor.

    DDL/DML auto-detection and ``RETURNING`` eager materialization happen
    inside the Go driver; this class only restores two 1.x DBAPI
    niceties — ``description`` is ``None`` after a DDL/DML statement (the
    driver reports an empty schema, which this shim collapses to "no
    result set"), and ``execute_update()`` returns the affected-row
    count directly — and relaxes one: fetching after a successfully
    executed DDL/DML statement returns an empty result (``None``/``[]``)
    instead of raising, matching ``sqlite3``/``duckdb``. Generic DB-API
    consumers (e.g. sqlframe) call ``fetchall()`` unconditionally after
    ``execute()`` and only then inspect ``description``; strict raising
    breaks them. Fetching before any ``execute()`` still raises.
    """

    # True after an execute that produced no result set (DDL/DML);
    # class-level default covers cursors that have never executed.
    _executed_without_result_set = False

    def execute(self, operation, parameters=None):
        """Execute a query; DDL/DML executes immediately on the server.

        Returns this cursor (to enable method chaining).
        """
        self._executed_without_result_set = False
        super().execute(operation, parameters)
        # The Go driver signals "statement executed, no result set" with
        # an empty schema. Collapse it so description is None, as in 1.x.
        # (Reading .description does not consume the stream.)
        if self._results is not None and len(self._results.description) == 0:
            self._results.close()
            self._results = None
            self._executed_without_result_set = True
        return self

    def execute_update(self, query: str) -> int:
        """Execute a DDL/DML statement immediately and return rows affected."""
        self.adbc_statement.set_sql_query(query)
        rowcount = self.adbc_statement.execute_update()
        self._rowcount = rowcount
        self._results = None
        self._last_query = query
        self._executed_without_result_set = True
        return rowcount

    def fetchone(self):
        """Fetch one row, or ``None`` after a statement with no result set."""
        if self._results is None and self._executed_without_result_set:
            return None
        return super().fetchone()

    def fetchmany(self, size=None):
        """Fetch some rows, or ``[]`` after a statement with no result set."""
        if self._results is None and self._executed_without_result_set:
            return []
        if size is not None:
            return super().fetchmany(size)
        return super().fetchmany()

    def fetchall(self):
        """Fetch all rows, or ``[]`` after a statement with no result set."""
        if self._results is None and self._executed_without_result_set:
            return []
        return super().fetchall()


class Connection(_BaseConnection):
    """GizmoSQL connection that returns :class:`Cursor` instances."""

    # Retained from 1.x: cache adbc_get_info() results. The Go driver
    # already serializes GetInfo internally; the cache preserves the 1.x
    # exactly-once behavior.
    _get_info_lock = threading.Lock()
    _get_info_cache: Optional[Dict[Union[str, int], Any]] = None

    def adbc_get_info(self) -> Dict[Union[str, int], Any]:
        """Get metadata about the database and driver (thread-safe, cached)."""
        if self._get_info_cache is None:
            with self._get_info_lock:
                if self._get_info_cache is None:
                    Connection._get_info_cache = super().adbc_get_info()
        return self._get_info_cache

    def cursor(
        self,
        *,
        adbc_stmt_kwargs: Optional[dict[str, Any]] = None,
    ) -> Cursor:
        """Create a new :class:`Cursor` for querying the database."""
        cursor = Cursor(self, adbc_stmt_kwargs, dbapi_backend=self._backend)
        self._cursors.add(cursor)
        return cursor


def connect(
    uri: Optional[str] = None,
    *,
    profile: Optional[str] = None,
    username: Optional[str] = None,
    password: Optional[str] = None,
    tls_skip_verify: bool = False,
    auth_type: str = "password",
    oauth_port: int = DEFAULT_OAUTH_PORT,
    oauth_url: Optional[str] = None,
    oauth_tls_skip_verify: Optional[bool] = None,
    oauth_timeout: int = 300,
    open_browser: bool = True,
    catalog: Optional[str] = None,
    db_schema: Optional[str] = None,
    db_kwargs: Optional[dict[str, str]] = None,
    conn_kwargs: Optional[dict[str, str]] = None,
    autocommit: bool = True,
) -> Connection:
    """Connect to a GizmoSQL server (DBAPI 2.0).

    Same signature and semantics as the 1.x driver. At least one of
    ``uri`` or ``profile`` must be provided. ``gizmosql://host:port``
    URIs are TLS-by-default (``?transport=tcp`` for plaintext); the
    legacy ``grpc+tls://`` / ``grpc+tcp://`` / ``flightsql://`` schemes
    are also accepted.

    Args:
        uri: Server URI. Optional if ``profile`` supplies the URI. A
            ``profile://<name>`` URI is also accepted.
        profile: Name (or absolute path) of an ADBC connection profile.
        username: Username for password auth.
        password: Password for password auth.
        tls_skip_verify: Skip TLS certificate verification.
        auth_type: ``"password"`` (default) or ``"external"`` (OAuth/SSO
            browser flow, performed client-side exactly as in 1.x).
        oauth_port: OAuth HTTP port (default 31339).
        oauth_url: Explicit OAuth base URL (else auto-discovered).
        oauth_tls_skip_verify: TLS skip for the OAuth server (defaults to
            ``tls_skip_verify``).
        oauth_timeout: Seconds to wait for OAuth completion.
        open_browser: Automatically open the browser for OAuth.
        catalog: Catalog (DuckDB database) to make current for the
            session, applied at connect time — shorthand for
            ``conn_kwargs={"adbc.connection.catalog": ...}``. The catalog
            must already be attached on the server.
        db_schema: Schema to make current for the session — shorthand
            for ``conn_kwargs={"adbc.connection.db_schema": ...}``.
        db_kwargs: Extra ADBC database options.
        conn_kwargs: Extra ADBC connection options.
        autocommit: Enable autocommit (default True).

    Returns:
        A DBAPI 2.0 :class:`Connection`.
    """
    if uri is None and profile is None:
        raise ValueError("Must provide at least one of 'uri' or 'profile'.")

    if db_kwargs is None:
        db_kwargs = {}
    else:
        db_kwargs = dict(db_kwargs)

    if conn_kwargs is None:
        conn_kwargs = {}
    else:
        conn_kwargs = dict(conn_kwargs)

    if tls_skip_verify:
        db_kwargs.setdefault(DatabaseOptions.TLS_SKIP_VERIFY.value, "true")

    _conn_opts = adbc_driver_manager.ConnectionOptions
    if catalog is not None:
        conn_kwargs.setdefault(_conn_opts.CURRENT_CATALOG.value, catalog)
    if db_schema is not None:
        conn_kwargs.setdefault(_conn_opts.CURRENT_DB_SCHEMA.value, db_schema)

    if oauth_tls_skip_verify is None:
        oauth_tls_skip_verify = tls_skip_verify

    if profile is not None:
        db_kwargs.setdefault("profile", profile)

    if auth_type == "external":
        if oauth_url is None and (uri is None or uri.startswith("profile://")):
            raise ValueError(
                "auth_type='external' with a connection profile requires either "
                "an explicit Flight SQL 'uri' or an 'oauth_url' for OAuth discovery."
            )
        host = _extract_host(uri) if uri is not None else ""
        result = get_oauth_token(
            host=host,
            port=oauth_port,
            tls_skip_verify=oauth_tls_skip_verify,
            timeout=oauth_timeout,
            open_browser=open_browser,
            oauth_url=oauth_url,
        )
        db_kwargs["username"] = "token"
        db_kwargs["password"] = result.token

    elif auth_type == "password":
        if username is not None:
            db_kwargs.setdefault("username", username)
        if password is not None:
            db_kwargs.setdefault("password", password)
    else:
        raise ValueError(f"Invalid auth_type: {auth_type!r}. Must be 'password' or 'external'.")

    if uri is not None and not uri.startswith("profile://"):
        db_kwargs.setdefault("uri", uri)
    elif uri is not None:
        # profile://<name> URIs are the profile mechanism in disguise.
        db_kwargs.setdefault("profile", uri[len("profile://") :])

    db = None
    conn = None
    try:
        db = adbc_driver_manager.AdbcDatabase(driver=_driver_path(), **db_kwargs)
        conn = adbc_driver_manager.AdbcConnection(db, **(conn_kwargs or {}))
        return Connection(db, conn, autocommit=autocommit)
    except Exception:
        if conn:
            conn.close()
        if db:
            db.close()
        raise


def execute_update(cursor: Cursor, query: str) -> int:
    """Execute a DDL/DML statement immediately (module-level 1.x compat).

    Equivalent to ``cursor.execute_update(query)``.
    """
    return cursor.execute_update(query)


def _extract_host(uri: str) -> str:
    """Extract the hostname from a connection URI."""
    if "://" in uri:
        remainder = uri.split("://", 1)[1]
    else:
        remainder = uri

    # Remove path and query string (e.g. gizmosql://host:port?transport=tcp)
    remainder = remainder.split("/", 1)[0].split("?", 1)[0]

    # Remove port
    if ":" in remainder:
        host = remainder.rsplit(":", 1)[0]
    else:
        host = remainder

    return host.rstrip("/")
