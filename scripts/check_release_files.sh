#!/usr/bin/env bash
# Check a release's files and write their checksums:
#   - exactly six library archives, one per platform, and six wheels;
#   - every archive libadbc_driver_gizmosql-<ref>-<platform>.tar.gz unpacks to one folder of the
#     same name holding the platform's library, built for that platform (the layout
#     install.adbc.gizmodata.com, github.com/gizmodata/adbc-install, unpacks; scripts/package_lib.sh
#     writes it);
#   - writes <dir>/SHA256SUMS over every archive and wheel and verifies it with `sha256sum -c`.
#
#   scripts/check_release_files.sh <dir> <ref>    # release.yml: dist "$GITHUB_REF_NAME"
set -euo pipefail

[ $# -eq 2 ] || { echo "usage: $0 <dir> <ref>" >&2; exit 2; }
lib=libadbc_driver_gizmosql
ref=${2//\//-}  # a branch name may hold slashes (package_lib.sh does the same)
cd "$1"

fail() { echo "::error::$*" >&2; exit 1; }
sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

n=$(find . -maxdepth 1 -name "$lib-*.tar.gz" | wc -l)
[ "$n" -eq 6 ] || fail "$n library archives, expected 6"
n=$(find . -maxdepth 1 -name '*.whl' | wc -l)
[ "$n" -eq 6 ] || fail "$n wheels, expected 6"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
for platform in linux_amd64 linux_arm64 macos_amd64 macos_arm64 windows_amd64 windows_arm64; do
  case "$platform" in
    linux_amd64) ext=so; kind='ELF 64-bit.*x86-64' ;;
    linux_arm64) ext=so; kind='ELF 64-bit.*(aarch64|arm64)' ;;
    macos_amd64) ext=dylib; kind='Mach-O 64-bit.*x86_64' ;;
    macos_arm64) ext=dylib; kind='Mach-O 64-bit.*arm64' ;;
    windows_amd64) ext=dll; kind='PE32\+.*DLL.*x86-64' ;;
    windows_arm64) ext=dll; kind='PE32\+.*DLL.*(aarch64|arm64)' ;;
  esac
  top="$lib-$ref-$platform"
  [ -f "$top.tar.gz" ] || fail "no $top.tar.gz"
  rm -rf "${tmp:?}"/*
  tar -xzf "$top.tar.gz" -C "$tmp"
  [ "$(ls -A "$tmp")" = "$top" ] || fail "$top.tar.gz does not unpack to the one folder $top/: $(ls -A "$tmp" | tr '\n' ' ')"
  [ -s "$tmp/$top/$lib.$ext" ] || fail "$top.tar.gz has no $top/$lib.$ext"
  what=$(file -b "$tmp/$top/$lib.$ext")
  grep -Eiq -- "$kind" <<<"$what" || fail "$top/$lib.$ext is not a $platform library: $what"
  echo "ok  $top.tar.gz: $top/$lib.$ext ($what)"
done

sha256 -- "$lib"-*.tar.gz *.whl > SHA256SUMS
sha256 -c SHA256SUMS
[ "$(wc -l < SHA256SUMS)" -eq 12 ] || fail "SHA256SUMS has $(wc -l < SHA256SUMS) lines, expected 12"
cat SHA256SUMS
