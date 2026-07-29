"""Option-key constants for the GizmoSQL ADBC driver.

Vendored from ``adbc_driver_flightsql`` 1.12.0 (Apache-2.0) so the 2.0
bindings need no dependency on that package — the underlying Flight SQL
option keys are a stable wire-level contract of the bundled Go driver.
"""

import enum


class DatabaseOptions(enum.Enum):
    """Database options specific to the Flight SQL driver."""

    #: The authorization header to use for requests.
    AUTHORIZATION_HEADER = "adbc.flight.sql.authorization_header"
    #: Server name in authentication handshake
    AUTHORITY = "adbc.flight.sql.client_option.authority"
    #: Enable mTLS and use these PEM-encoded certificates.
    MTLS_CERT_CHAIN = "adbc.flight.sql.client_option.mtls_cert_chain"
    #: Enable mTLS and use this PEM-encoded private key.
    MTLS_PRIVATE_KEY = "adbc.flight.sql.client_option.mtls_private_key"
    #: Add an arbitrary header to all outgoing requests.
    #:
    #: This option should prefix the name of the header to add
    #: (i.e. it should be used like
    #: ``f"{DatabaseOptions.RpcCallHeaderPrefix}.x-my-header"``).
    RPC_CALL_HEADER_PREFIX = "adbc.flight.sql.rpc.call_header."
    #: Set a timeout on calls that fetch data (in floating-point seconds).
    #:
    #: This corresponds to Flight RPC DoGet calls.
    TIMEOUT_FETCH = "adbc.flight.sql.rpc.timeout_seconds.fetch"
    #: Set a timeout on calls that execute queries (in floating-point
    #: seconds).
    #:
    #: This corresponds to Flight RPC GetFlightInfo calls.
    TIMEOUT_QUERY = "adbc.flight.sql.rpc.timeout_seconds.query"
    #: Set a timeout on calls that upload or update data (in
    #: floating-point seconds).
    TIMEOUT_UPDATE = "adbc.flight.sql.rpc.timeout_seconds.update"
    #: Override the hostname used for TLS.
    TLS_OVERRIDE_HOSTNAME = "adbc.flight.sql.client_option.tls_override_hostname"
    #: Use these PEM-encoded root certificates for TLS.
    TLS_ROOT_CERTS = "adbc.flight.sql.client_option.tls_root_certs"
    #: Do not verify the server's TLS certificate.
    TLS_SKIP_VERIFY = "adbc.flight.sql.client_option.tls_skip_verify"
    #: Block and wait for the connection to be established.
    WITH_BLOCK = "adbc.flight.sql.client_option.with_block"
    #: Enable cookie middleware. Default is disabled ("false")
    WITH_COOKIE_MIDDLEWARE = "adbc.flight.sql.rpc.with_cookie_middleware"
    #: Set the maximum gRPC message size (in bytes). The default is 16 MiB.
    WITH_MAX_MSG_SIZE = "adbc.flight.sql.client_option.with_max_msg_size"

    # OAuth 2.0 options

    #: Specifies the OAuth 2.0 flow type to use.
    #:
    #: See :class:`OAuthFlowType` for possible values.
    OAUTH_FLOW = "adbc.flight.sql.oauth.flow"
    #: The authorization endpoint URL for OAuth 2.0.
    OAUTH_AUTH_URI = "adbc.flight.sql.oauth.auth_uri"
    #: The endpoint URL where the client application requests tokens
    #: from the authorization server.
    OAUTH_TOKEN_URI = "adbc.flight.sql.oauth.token_uri"
    #: The redirect URI for OAuth 2.0 flows.
    OAUTH_REDIRECT_URI = "adbc.flight.sql.oauth.redirect_uri"
    #: Space-separated list of permissions that the client is requesting
    #: access to (e.g., ``"read.all offline_access"``).
    OAUTH_SCOPE = "adbc.flight.sql.oauth.scope"
    #: Unique identifier issued to the client application by the
    #: authorization server.
    OAUTH_CLIENT_ID = "adbc.flight.sql.oauth.client_id"
    #: Secret associated with the client_id. Used to authenticate the
    #: client application to the authorization server.
    OAUTH_CLIENT_SECRET = "adbc.flight.sql.oauth.client_secret"

    # OAuth 2.0 Token Exchange options (RFC 8693)

    #: The security token that the client application wants to exchange.
    OAUTH_EXCHANGE_SUBJECT_TOKEN = "adbc.flight.sql.oauth.exchange.subject_token"
    #: Identifier for the type of the subject token.
    #:
    #: See :class:`OAuthTokenType` for supported token types.
    OAUTH_EXCHANGE_SUBJECT_TOKEN_TYPE = "adbc.flight.sql.oauth.exchange.subject_token_type"
    #: A security token that represents the identity of the acting party.
    OAUTH_EXCHANGE_ACTOR_TOKEN = "adbc.flight.sql.oauth.exchange.actor_token"
    #: Identifier for the type of the actor token.
    #:
    #: See :class:`OAuthTokenType` for supported token types.
    OAUTH_EXCHANGE_ACTOR_TOKEN_TYPE = "adbc.flight.sql.oauth.exchange.actor_token_type"
    #: The type of token the client wants to receive in exchange.
    #:
    #: See :class:`OAuthTokenType` for supported token types.
    OAUTH_EXCHANGE_REQUESTED_TOKEN_TYPE = "adbc.flight.sql.oauth.exchange.requested_token_type"
    #: Specific permissions requested for the new token in token exchange.
    OAUTH_EXCHANGE_SCOPE = "adbc.flight.sql.oauth.exchange.scope"
    #: The intended audience for the requested security token in token exchange.
    OAUTH_EXCHANGE_AUD = "adbc.flight.sql.oauth.exchange.aud"
    #: The resource server where the client intends to use the requested
    #: security token in token exchange.
    OAUTH_EXCHANGE_RESOURCE = "adbc.flight.sql.oauth.exchange.resource"


class ConnectionOptions(enum.Enum):
    """Connection options specific to the Flight SQL driver."""

    #: Add an arbitrary header to all outgoing requests.
    #:
    #: This option should prefix the name of the header to add
    #: (i.e. it should be used like
    #: ``f"{ConnectionOptions.RPC_CALL_HEADER_PREFIX}x-my-header"``).
    #:
    #: Overrides any headers set via the equivalent database option.
    RPC_CALL_HEADER_PREFIX = DatabaseOptions.RPC_CALL_HEADER_PREFIX.value
    #: Get all session options as a JSON key-value blob.
    OPTION_SESSION_OPTIONS = "adbc.flight.sql.session.options"
    #: Get or set a session option.
    OPTION_SESSION_OPTION_PREFIX = "adbc.flight.sql.session.option."
    #: Erase a session option (use "" as the value).
    OPTION_ERASE_SESSION_OPTION_PREFIX = "adbc.flight.sql.session.optionerase."
    #: Get or set a boolean valued session option.
    OPTION_BOOL_SESSION_OPTION_PREFIX = "adbc.flight.sql.session.optionbool."
    #: Get or set a string-list-valued session option as a JSON array.
    OPTION_STRING_LIST_SESSION_OPTION_PREFIX = "adbc.flight.sql.session.optionstringlist."
    #: Set a timeout on calls that fetch data (in floating-point seconds).
    #:
    #: This corresponds to Flight RPC DoGet calls.
    TIMEOUT_FETCH = DatabaseOptions.TIMEOUT_FETCH.value
    #: Set a timeout on calls that execute queries (in floating-point
    #: seconds).
    #:
    #: This corresponds to Flight RPC GetFlightInfo calls.
    TIMEOUT_QUERY = DatabaseOptions.TIMEOUT_QUERY.value
    #: Set a timeout on calls that upload or update data (in
    #: floating-point seconds).
    TIMEOUT_UPDATE = DatabaseOptions.TIMEOUT_UPDATE.value
