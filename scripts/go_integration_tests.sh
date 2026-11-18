#!/usr/bin/env bash
# Run the Go live-server tests (TestIntegration*) and fail unless every one of them ran and passed.
# They t.Skip when no gizmosql_server is found (GIZMOSQL_SERVER_BIN, PATH, the gizmosql cache), so a
# plain `go test` would be green with nothing tested. ci.yml's go job runs this.
#
#   GIZMOSQL_SERVER_BIN=<path to gizmosql_server> scripts/go_integration_tests.sh
set -euo pipefail
cd "$(dirname "$0")/.."

fail() { echo "::error::$*" >&2; exit 1; }

log=$(mktemp)
trap 'rm -f "$log"' EXIT
(cd go && go test -v -count=1 -run Integration ./...) 2>&1 | tee "$log"

if grep -q -- '--- SKIP: TestIntegration' "$log"; then
  grep -- '--- SKIP: TestIntegration' "$log" >&2
  fail "live-server tests were skipped"
fi
want=$(cat go/gizmosql/*_test.go | grep -c '^func TestIntegration' || true)
passed=$(grep -c -- '^--- PASS: TestIntegration' "$log" || true)
[ "$want" -gt 0 ] || fail "found no TestIntegration functions in go/gizmosql"
[ "$passed" -eq "$want" ] || fail "$passed of $want TestIntegration tests passed"
echo "all $passed TestIntegration tests ran and passed (GIZMOSQL_SERVER_BIN=${GIZMOSQL_SERVER_BIN:-unset})"
