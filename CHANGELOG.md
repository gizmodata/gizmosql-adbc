# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

Versioning note: this repo's releases continue the `adbc-driver-gizmosql`
line — the first release from this repo will be 2.0.0 (the Go driver plus
Python bindings), succeeding the 1.x pure-Python driver.

## [Unreleased]

### Added
- Repository scaffold: Go driver + Python bindings monorepo layout,
  design plan (`docs/plan.md`), and work plan (`docs/WORKPLAN.md`).
- Go module `github.com/gizmodata/gizmosql-adbc/go` pinned to
  `arrow-adbc/go/adbc` v1.12.0: `gizmosql.NewDriver(alloc)` pass-through
  driver wrapping the upstream Flight SQL driver with Database /
  Connection / Statement interception points, and the `gizmosql://` URI
  scheme (TLS by default, `?transport=tcp` for plaintext) rewritten onto
  `flightsql://`. Unit tests cover the URI rewrite, option-map
  non-mutation, downstream delegation (via a recording fake), and
  real-driver option validation.
