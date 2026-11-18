#!/usr/bin/env bash
# Check a release's files and write their checksums:
#   - exactly the six library archives and six wheels;
#   - every archive libadbc_driver_gizmosql-<ref>-<platform>.tar.gz holds one top folder of the
#     same name, and that folder holds the platform's library (the layout install.adbc.gizmodata.com
#     and scripts/package_lib.sh rely on);
#   - writes <dir>/SHA256SUMS over every archive and wheel and verifies it with `sha256sum -c`.
#
#   scripts/check_release_files.sh <dir>    # release.yml: dist
set -euo pipefail

[ $# -eq 1 ] || { echo "usage: $0 <dir>" >&2; exit 2; }
cd "$1"

sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

shopt -s nullglob
archives=(libadbc_driver_gizmosql-*.tar.gz)
wheels=(*.whl)
if [ "${#archives[@]}" -ne 6 ] || [ "${#wheels[@]}" -ne 6 ]; then
  echo "::error::expected 6 library archives and 6 wheels, found ${#archives[@]} and ${#wheels[@]}" >&2
  exit 1
fi

for a in "${archives[@]}"; do
  top=${a%.tar.gz}
  case "$top" in
    *-linux_*) lib=libadbc_driver_gizmosql.so ;;
    *-macos_*) lib=libadbc_driver_gizmosql.dylib ;;
    *-windows_*) lib=libadbc_driver_gizmosql.dll ;;
    *) echo "::error::$a: no platform in its name" >&2; exit 1 ;;
  esac
  entries=$(tar -tzf "$a")
  # Every entry sits under the one top folder named like the archive.
  outside=$(grep -v -e "^$top/" -e "^$top\$" <<<"$entries" || true)
  if [ -n "$outside" ]; then
    echo "::error::$a: entries outside $top/:" >&2
    printf '%s\n' "$outside" >&2
    exit 1
  fi
  if ! grep -qxF -- "$top/$lib" <<<"$entries"; then
    echo "::error::$a: $top/$lib is missing" >&2
    printf '%s\n' "$entries" >&2
    exit 1
  fi
  echo "ok  $a  ($top/$lib)"
done

sha256 -- "${archives[@]}" "${wheels[@]}" > SHA256SUMS
sha256 -c SHA256SUMS
if [ "$(wc -l < SHA256SUMS)" -ne 12 ]; then
  echo "::error::SHA256SUMS should list 12 files" >&2
  exit 1
fi
cat SHA256SUMS
