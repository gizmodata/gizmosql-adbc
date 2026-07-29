"""ADBC driver for GizmoSQL with OAuth/SSO support.

Version 2.0 of this package is a thin Python binding over the native Go
GizmoSQL ADBC driver (``libadbc_driver_gizmosql``), keeping the 1.x API
byte-compatible. GizmoSQL-specific execution semantics — the
``gizmosql://`` URI scheme, DDL/DML immediate execution, and
``RETURNING`` eager materialization — live inside the shared library,
shared by every ADBC language.

Quick start::

    from adbc_driver_gizmosql import dbapi as gizmosql

    # Password authentication
    with gizmosql.connect("gizmosql://localhost:31337",
                          username="user", password="pass",
                          tls_skip_verify=True) as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT 1")
            print(cur.fetch_arrow_table())

    # OAuth/SSO authentication
    with gizmosql.connect("gizmosql://localhost:31337",
                          auth_type="external",
                          tls_skip_verify=True) as conn:
        with conn.cursor() as cur:
            cur.execute("SELECT CURRENT_USER")
            print(cur.fetch_arrow_table())
"""

from __future__ import annotations

from ._oauth import GizmoSQLOAuthError, OAuthResult, get_oauth_token
from ._options import ConnectionOptions, DatabaseOptions
from ._version import __version__

__all__ = [
    "ConnectionOptions",
    "DatabaseOptions",
    "GizmoSQLOAuthError",
    "OAuthResult",
    "__version__",
    "get_oauth_token",
]
