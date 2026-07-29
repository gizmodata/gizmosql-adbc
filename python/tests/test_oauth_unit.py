"""Unit tests for the OAuth flow module (no Docker required)."""

from __future__ import annotations

import json
from unittest.mock import MagicMock, patch

import pytest

from adbc_driver_gizmosql._oauth import (
    GizmoSQLOAuthError,
    OAuthResult,
    _discover_oauth_base_url,
    _http_get_json,
    get_oauth_token,
)


class TestHttpGetJson:
    """Tests for _http_get_json."""

    def test_success(self):
        response_data = {"session_uuid": "abc-123", "auth_url": "https://example.com/auth"}
        mock_response = MagicMock()
        mock_response.read.return_value = json.dumps(response_data).encode("utf-8")
        mock_response.__enter__ = MagicMock(return_value=mock_response)
        mock_response.__exit__ = MagicMock(return_value=False)

        with patch("urllib.request.urlopen", return_value=mock_response):
            result = _http_get_json("https://localhost:31339/oauth/initiate")

        assert result == response_data

    def test_http_error(self):
        import urllib.error

        exc = urllib.error.HTTPError(
            "https://localhost:31339/oauth/initiate",
            500,
            "Internal Server Error",
            {},
            MagicMock(read=MagicMock(return_value=b"error body")),
        )

        with patch("urllib.request.urlopen", side_effect=exc):
            with pytest.raises(GizmoSQLOAuthError, match="HTTP 500"):
                _http_get_json("https://localhost:31339/oauth/initiate")

    def test_connection_error(self):
        import urllib.error

        exc = urllib.error.URLError("Connection refused")

        with patch("urllib.request.urlopen", side_effect=exc):
            with pytest.raises(GizmoSQLOAuthError, match="Failed to connect"):
                _http_get_json("https://localhost:31339/oauth/initiate")


class TestDiscoverOAuthBaseUrl:
    """Tests for OAuth URL auto-discovery."""

    def test_https_success(self):
        response_data = {"session_uuid": "uuid-1", "auth_url": "https://idp.example.com/auth"}

        with patch(
            "adbc_driver_gizmosql._oauth._http_get_json", return_value=response_data
        ) as mock_get:
            base_url, data = _discover_oauth_base_url("localhost", 31339, tls_skip_verify=True)

        assert base_url == "https://localhost:31339"
        assert data == response_data
        mock_get.assert_called_once()
        assert mock_get.call_args[0][0] == "https://localhost:31339/oauth/initiate"

    def test_https_fails_http_succeeds(self):
        response_data = {"session_uuid": "uuid-2", "auth_url": "https://idp.example.com/auth"}

        def mock_get(url, ssl_context=None):
            if url.startswith("https://"):
                raise GizmoSQLOAuthError("Connection refused")
            return response_data

        with patch("adbc_driver_gizmosql._oauth._http_get_json", side_effect=mock_get):
            base_url, data = _discover_oauth_base_url("localhost", 31339, tls_skip_verify=True)

        assert base_url == "http://localhost:31339"
        assert data == response_data

    def test_both_fail(self):
        def mock_get(url, ssl_context=None):
            raise GizmoSQLOAuthError("Connection refused")

        with patch("adbc_driver_gizmosql._oauth._http_get_json", side_effect=mock_get):
            with pytest.raises(GizmoSQLOAuthError, match="Could not connect"):
                _discover_oauth_base_url("localhost", 31339, tls_skip_verify=True)


class TestGetOAuthToken:
    """Tests for the full OAuth flow."""

    def _mock_initiate_response(self):
        return {
            "session_uuid": "test-uuid-1234",
            "auth_url": "https://accounts.google.com/o/oauth2/auth?client_id=xxx",
        }

    def test_successful_flow(self):
        initiate_data = self._mock_initiate_response()
        poll_responses = [
            {"status": "pending"},
            {"status": "pending"},
            {"status": "complete", "token": "eyJhbGciOiJSUzI1NiJ9.test-jwt-token"},
        ]
        poll_iter = iter(poll_responses)

        def mock_get(url, ssl_context=None):
            if "/oauth/initiate" in url:
                return initiate_data
            return next(poll_iter)

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
            patch("adbc_driver_gizmosql._oauth._http_get_json", side_effect=mock_get),
            patch("webbrowser.open") as mock_browser,
            patch("time.sleep"),
        ):
            mock_discover.return_value = ("https://localhost:31339", initiate_data)

            result = get_oauth_token("localhost", poll_interval=0.01)

        assert isinstance(result, OAuthResult)
        assert result.token == "eyJhbGciOiJSUzI1NiJ9.test-jwt-token"
        assert result.session_uuid == "test-uuid-1234"
        mock_browser.assert_called_once_with(initiate_data["auth_url"])

    def test_no_browser(self):
        initiate_data = self._mock_initiate_response()
        complete_response = {"status": "complete", "token": "jwt-token"}

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
            patch("adbc_driver_gizmosql._oauth._http_get_json", return_value=complete_response),
            patch("webbrowser.open") as mock_browser,
            patch("builtins.print") as mock_print,
        ):
            mock_discover.return_value = ("https://localhost:31339", initiate_data)

            result = get_oauth_token("localhost", open_browser=False)

        mock_browser.assert_not_called()
        mock_print.assert_called_once()
        assert "authenticate" in mock_print.call_args[0][0].lower() or \
               "browser" in mock_print.call_args[0][0].lower() or \
               "http" in mock_print.call_args[0][0].lower()
        assert result.token == "jwt-token"

    def test_timeout(self):
        initiate_data = self._mock_initiate_response()
        pending_response = {"status": "pending"}

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
            patch("adbc_driver_gizmosql._oauth._http_get_json", return_value=pending_response),
            patch("webbrowser.open"),
            patch("time.sleep"),
            patch("time.monotonic") as mock_time,
        ):
            mock_discover.return_value = ("https://localhost:31339", initiate_data)
            # First call returns 0, subsequent calls return past timeout
            mock_time.side_effect = [0, 0, 301]

            with pytest.raises(GizmoSQLOAuthError, match="timed out"):
                get_oauth_token("localhost", timeout=300, poll_interval=0.01)

    def test_error_status(self):
        initiate_data = self._mock_initiate_response()
        error_response = {"status": "error", "error": "User denied access"}

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
            patch("adbc_driver_gizmosql._oauth._http_get_json", return_value=error_response),
            patch("webbrowser.open"),
        ):
            mock_discover.return_value = ("https://localhost:31339", initiate_data)

            with pytest.raises(GizmoSQLOAuthError, match="User denied access"):
                get_oauth_token("localhost")

    def test_not_found_status(self):
        initiate_data = self._mock_initiate_response()
        not_found_response = {"status": "not_found"}

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
            patch("adbc_driver_gizmosql._oauth._http_get_json", return_value=not_found_response),
            patch("webbrowser.open"),
        ):
            mock_discover.return_value = ("https://localhost:31339", initiate_data)

            with pytest.raises(GizmoSQLOAuthError, match="not found"):
                get_oauth_token("localhost")

    def test_explicit_oauth_url(self):
        initiate_data = self._mock_initiate_response()
        complete_response = {"status": "complete", "token": "jwt-token"}

        call_urls = []

        def mock_get(url, ssl_context=None):
            call_urls.append(url)
            if "/oauth/initiate" in url:
                return initiate_data
            return complete_response

        with (
            patch("adbc_driver_gizmosql._oauth._http_get_json", side_effect=mock_get),
            patch("webbrowser.open"),
        ):
            result = get_oauth_token(
                "localhost",
                oauth_url="http://localhost:9999",
            )

        assert result.token == "jwt-token"
        assert call_urls[0] == "http://localhost:9999/oauth/initiate"
        assert call_urls[1] == "http://localhost:9999/oauth/token/test-uuid-1234"

    def test_missing_session_uuid(self):
        bad_initiate = {"auth_url": "https://idp.example.com/auth"}

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
        ):
            mock_discover.return_value = ("https://localhost:31339", bad_initiate)

            with pytest.raises(GizmoSQLOAuthError, match="Unexpected response"):
                get_oauth_token("localhost")

    def test_missing_auth_url(self):
        bad_initiate = {"session_uuid": "uuid-1"}

        with (
            patch("adbc_driver_gizmosql._oauth._discover_oauth_base_url") as mock_discover,
        ):
            mock_discover.return_value = ("https://localhost:31339", bad_initiate)

            with pytest.raises(GizmoSQLOAuthError, match="Unexpected response"):
                get_oauth_token("localhost")
