"""OAuth browser flow for GizmoSQL server-side code exchange.

This module handles the OAuth/SSO browser flow by communicating with
GizmoSQL's built-in OAuth HTTP endpoints. It uses only stdlib modules
(no external dependencies beyond Python itself).

Flow:
    1. GET /oauth/initiate → {session_uuid, auth_url}
    2. Open auth_url in browser
    3. Poll GET /oauth/token/{uuid} until complete
    4. Return the identity token for Flight SQL Basic Auth
"""

from __future__ import annotations

import json
import ssl
import time
import urllib.error
import urllib.request
import webbrowser
from dataclasses import dataclass
from typing import Optional

DEFAULT_OAUTH_PORT = 31339
DEFAULT_POLL_INTERVAL = 1  # seconds
DEFAULT_TIMEOUT = 300  # seconds


class GizmoSQLOAuthError(Exception):
    """Raised when the OAuth flow fails."""


@dataclass
class OAuthResult:
    """Result of a successful OAuth flow.

    Attributes:
        token: The identity token (JWT) from the IdP.
        session_uuid: The session UUID used during the OAuth flow.
    """

    token: str
    session_uuid: str


def _make_ssl_context(tls_skip_verify: bool) -> ssl.SSLContext:
    """Create an SSL context, optionally skipping certificate verification."""
    if tls_skip_verify:
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        return ctx
    return ssl.create_default_context()


def _http_get_json(url: str, ssl_context: Optional[ssl.SSLContext] = None) -> dict:
    """Perform an HTTP GET and parse the JSON response."""
    req = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(req, context=ssl_context) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise GizmoSQLOAuthError(f"HTTP {exc.code} from {url}: {body}") from exc
    except urllib.error.URLError as exc:
        raise GizmoSQLOAuthError(f"Failed to connect to {url}: {exc.reason}") from exc


def _discover_oauth_base_url(
    host: str,
    port: int,
    tls_skip_verify: bool,
) -> tuple[str, dict]:
    """Discover the OAuth base URL by probing the server.

    Tries HTTPS first, falls back to HTTP on connection error.
    Returns (base_url, initiate_response) so the /oauth/initiate
    response is reused (no wasted round trip).
    """
    ssl_context = _make_ssl_context(tls_skip_verify)

    # Try HTTPS first
    https_url = f"https://{host}:{port}/oauth/initiate"
    try:
        data = _http_get_json(https_url, ssl_context=ssl_context)
        base_url = f"https://{host}:{port}"
        return base_url, data
    except GizmoSQLOAuthError:
        pass

    # Fall back to HTTP
    http_url = f"http://{host}:{port}/oauth/initiate"
    try:
        data = _http_get_json(http_url)
        base_url = f"http://{host}:{port}"
        return base_url, data
    except GizmoSQLOAuthError as exc:
        raise GizmoSQLOAuthError(
            f"Could not connect to OAuth server at {host}:{port} (tried HTTPS and HTTP): {exc}"
        ) from exc


def get_oauth_token(
    host: str,
    port: int = DEFAULT_OAUTH_PORT,
    *,
    tls_skip_verify: bool = True,
    timeout: int = DEFAULT_TIMEOUT,
    poll_interval: float = DEFAULT_POLL_INTERVAL,
    open_browser: bool = True,
    oauth_url: Optional[str] = None,
) -> OAuthResult:
    """Perform the GizmoSQL OAuth browser flow and return an identity token.

    This initiates the OAuth flow with the GizmoSQL server, opens the
    authorization URL in the user's browser, and polls for completion.

    Args:
        host: GizmoSQL server hostname.
        port: OAuth HTTP port (default: 31339).
        tls_skip_verify: Skip TLS certificate verification for the OAuth
            server (default: True for localhost development).
        timeout: Maximum seconds to wait for the user to complete auth.
        poll_interval: Seconds between polling requests.
        open_browser: Whether to automatically open the auth URL in a browser.
        oauth_url: Explicit OAuth base URL (e.g., "https://gizmosql.example.com:31339").
            If not provided, auto-discovers by probing the server.

    Returns:
        OAuthResult with the identity token and session UUID.

    Raises:
        GizmoSQLOAuthError: If the OAuth flow fails or times out.
    """
    ssl_context = _make_ssl_context(tls_skip_verify)

    # Step 1: Initiate the OAuth flow
    if oauth_url is not None:
        base_url = oauth_url.rstrip("/")
        initiate_url = f"{base_url}/oauth/initiate"
        ctx = ssl_context if base_url.startswith("https") else None
        initiate_data = _http_get_json(initiate_url, ssl_context=ctx)
    else:
        base_url, initiate_data = _discover_oauth_base_url(host, port, tls_skip_verify)

    session_uuid = initiate_data.get("session_uuid")
    auth_url = initiate_data.get("auth_url")

    if not session_uuid or not auth_url:
        raise GizmoSQLOAuthError(f"Unexpected response from /oauth/initiate: {initiate_data}")

    # Step 2: Open the authorization URL in the browser
    if open_browser:
        webbrowser.open(auth_url)
    else:
        print(f"Open this URL in your browser to authenticate:\n{auth_url}")

    # Step 3: Poll for the token
    poll_url = f"{base_url}/oauth/token/{session_uuid}"
    poll_ssl_context = ssl_context if base_url.startswith("https") else None
    start_time = time.monotonic()

    while True:
        elapsed = time.monotonic() - start_time
        if elapsed >= timeout:
            raise GizmoSQLOAuthError(
                f"OAuth flow timed out after {timeout} seconds. "
                "The user did not complete authentication in time."
            )

        data = _http_get_json(poll_url, ssl_context=poll_ssl_context)
        status = data.get("status")

        if status == "complete":
            token = data.get("token")
            if not token:
                raise GizmoSQLOAuthError(f"Token poll returned 'complete' but no token: {data}")
            return OAuthResult(token=token, session_uuid=session_uuid)

        if status == "error":
            error_msg = data.get("error", "Unknown error")
            raise GizmoSQLOAuthError(f"OAuth flow failed: {error_msg}")

        if status == "not_found":
            raise GizmoSQLOAuthError(
                f"OAuth session {session_uuid} not found. It may have expired."
            )

        if status != "pending":
            raise GizmoSQLOAuthError(f"Unexpected token poll status: {status}")

        time.sleep(poll_interval)
