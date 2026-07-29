"""Shared test fixtures for adbc-driver-gizmosql.

Provides a session-scoped fixture that starts a GizmoSQL server via the
[gizmosql](https://pypi.org/project/gizmosql/) PyPI package. Unit tests
do not require any server.
"""

from __future__ import annotations

import datetime
from pathlib import Path

import pytest

GIZMOSQL_USERNAME = "gizmosql_username"
GIZMOSQL_PASSWORD = "gizmosql_password"


def _generate_self_signed_tls_cert(out_dir: Path) -> tuple[Path, Path]:
    """Mint a self-signed RSA cert + key for ``localhost`` so the test
    server's Flight SQL endpoint is reachable over ``grpc+tls://``."""
    from cryptography import x509  # noqa: PLC0415
    from cryptography.hazmat.primitives import hashes, serialization  # noqa: PLC0415
    from cryptography.hazmat.primitives.asymmetric import rsa  # noqa: PLC0415
    from cryptography.x509.oid import NameOID  # noqa: PLC0415

    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "localhost")])
    now = datetime.datetime.now(datetime.timezone.utc)
    cert = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=1))
        .not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(
            x509.SubjectAlternativeName([x509.DNSName("localhost")]),
            critical=False,
        )
        .sign(key, hashes.SHA256())
    )
    cert_path = out_dir / "tls_cert.pem"
    key_path = out_dir / "tls_key.pem"
    cert_path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    key_path.write_bytes(
        key.private_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PrivateFormat.PKCS8,
            encryption_algorithm=serialization.NoEncryption(),
        )
    )
    return cert_path, key_path


@pytest.fixture(scope="session")
def gizmosql_server(tmp_path_factory):
    """Start a GizmoSQL server as a managed subprocess via the gizmosql
    PyPI package. Auto-picks a free port and tears the server down on
    exit; no Docker required."""
    gizmosql = pytest.importorskip("gizmosql")

    tls_dir = tmp_path_factory.mktemp("tls")
    tls_dir.chmod(0o700)
    tls_cert, tls_key = _generate_self_signed_tls_cert(tls_dir)

    with gizmosql.Server(
        username=GIZMOSQL_USERNAME,
        password=GIZMOSQL_PASSWORD,
        extra_args=["--tls", str(tls_cert), str(tls_key)],
        extra_env={
            "PRINT_QUERIES": "1",
            "INIT_SQL_COMMANDS": "CALL dbgen(sf=0.01);",
        },
    ) as srv:
        yield srv


@pytest.fixture(scope="session")
def gizmosql_uri(gizmosql_server):
    """Return the Flight SQL URI for the test server."""
    return f"grpc+tls://{gizmosql_server.host}:{gizmosql_server.port}"
