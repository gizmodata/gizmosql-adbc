# gizmosql-adbc

[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--adbc-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-adbc)
[<img src="https://img.shields.io/badge/GitHub-gizmodata%2Fgizmosql--public-blue.svg?logo=Github">](https://github.com/gizmodata/gizmosql-public)
[![gizmosql-adbc-ci](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml/badge.svg)](https://github.com/gizmodata/gizmosql-adbc/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gizmodata/gizmosql-adbc/go.svg)](https://pkg.go.dev/github.com/gizmodata/gizmosql-adbc/go)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gizmodata/gizmosql-adbc?filename=go%2Fgo.mod)](go/go.mod)
[![License](https://img.shields.io/github/license/gizmodata/gizmosql-adbc)](LICENSE)

Native [ADBC](https://arrow.apache.org/adbc/) driver for
[GizmoSQL](https://gizmodata.com/gizmosql), written in Go, with Python
bindings — the successor to
[`adbc-driver-gizmosql`](https://github.com/gizmodata/adbc-driver-gizmosql) 1.x.

> **Status: pre-alpha / under active development.** The 1.x Python driver
> remains the supported release until this repo ships
> `adbc-driver-gizmosql` 2.0.

## Why a Go driver?

The GizmoSQL-specific behavior that today lives in the 1.x Python package —
DDL/DML auto-detection and immediate execution (GizmoSQL's lazy-execution
model), `RETURNING` handling, and the OAuth/SSO browser flow — moves into a
native Go ADBC driver built on
[`apache/arrow-adbc`](https://github.com/apache/arrow-adbc)'s Flight SQL
driver. Compiled to a C shared library, one implementation then serves
**every** ADBC language: Python, Go, R, C/C++, C#, Rust, and JavaScript.

## Layout

```
go/       Go driver (wraps arrow-adbc's flightsql driver) + cgo C exports
python/   Python bindings — ships libadbc_driver_gizmosql in its wheels,
          keeps the 1.x dbapi.connect() API byte-compatible (PyPI:
          adbc-driver-gizmosql 2.0)
docs/     Design plan and work plan
```

## Features (planned / ported from 1.x)

- `gizmosql://` URI scheme — TLS by default, `?transport=tcp` for plaintext
- DDL/DML auto-detection → immediate server-side execution (DoPut), with
  `INSERT/UPDATE/DELETE ... RETURNING` eagerly materialized on the query path
- OAuth/SSO code-exchange flow (`/oauth/initiate` → browser →
  `/oauth/token/{uuid}`), including a headless mode
- Everything the upstream Flight SQL driver provides: TLS, cookies,
  timeouts, connection profiles, OpenTelemetry tracing and logging

See [docs/plan.md](docs/plan.md) for the design and
[docs/WORKPLAN.md](docs/WORKPLAN.md) for build-out progress.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
