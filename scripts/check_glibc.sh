#!/usr/bin/env bash
# Fail if the Linux library needs a glibc newer than 2.17, or the repaired wheel's manylinux tag does
# (the gizmosql server enforces its own ceiling the same way).
set -euo pipefail
LIB="${1:?usage: check_glibc.sh <libadbc_driver_gizmosql.so> [wheel...]}"
shift
MAX=$(objdump -T "$LIB" | grep -o 'GLIBC_[0-9.]*' | sed 's/GLIBC_//' | sort -V | tail -1)
echo "highest glibc symbol version: ${MAX:-none}"
if [ -n "$MAX" ] && [ "$(printf '%s\n2.17\n' "$MAX" | sort -V | tail -1)" != "2.17" ]; then
  echo "::error::$LIB needs glibc $MAX (> 2.17)" >&2
  exit 1
fi
# The wheel's most compatible manylinux tag must need glibc <= 2.17 (manylinux1 = 2.5, manylinux2010 =
# 2.12, manylinux2014 = 2.17, manylinux_X_Y = X.Y); an amd64 build needing only 2.3.2 is tagged manylinux1.
for wheel in "$@"; do
  name=$(basename "$wheel")
  versions=$( (echo "$name" | grep -oE 'manylinux(1|2010|2014|_[0-9]+_[0-9]+)_' || true) \
    | sed -E 's/manylinux1_/2.5/; s/manylinux2010_/2.12/; s/manylinux2014_/2.17/; s/manylinux_([0-9]+)_([0-9]+)_/\1.\2/')
  floor=$(echo "$versions" | sort -V | head -1)
  if [ -z "$floor" ] || [ "$(printf '%s\n2.17\n' "$floor" | sort -V | tail -1)" != "2.17" ]; then
    echo "::error::$name needs glibc ${floor:-?} (> 2.17)" >&2
    exit 1
  fi
  echo "ok: $name (glibc >= $floor)"
done
