#!/usr/bin/env bash
# Fail if the Linux library needs a glibc newer than 2.17, or the repaired wheel isn't tagged
# manylinux_2_17 (the gizmosql server enforces its own ceiling the same way).
set -euo pipefail
LIB="${1:?usage: check_glibc.sh <libadbc_driver_gizmosql.so> [wheel...]}"
shift
MAX=$(objdump -T "$LIB" | grep -o 'GLIBC_[0-9.]*' | sed 's/GLIBC_//' | sort -V | tail -1)
echo "highest glibc symbol version: ${MAX:-none}"
if [ -n "$MAX" ] && [ "$(printf '%s\n2.17\n' "$MAX" | sort -V | tail -1)" != "2.17" ]; then
  echo "::error::$LIB needs glibc $MAX (> 2.17)" >&2
  exit 1
fi
for wheel in "$@"; do
  case "$(basename "$wheel")" in
    *manylinux_2_17_*) echo "ok: $(basename "$wheel")" ;;
    *) echo "::error::$(basename "$wheel") is not tagged manylinux_2_17" >&2; exit 1 ;;
  esac
done
