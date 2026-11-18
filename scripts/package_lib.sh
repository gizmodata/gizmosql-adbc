#!/usr/bin/env bash
# Package the built library (go/build/) as a release archive:
#   dist/libadbc_driver_gizmosql-<ref>-<platform>.tar.gz
# holding the library, the manifest template, LICENSE and NOTICE in a folder of the same name
# (scripts/check_release_files.sh checks that layout). release.yml uses it.
#
#   scripts/package_lib.sh <platform> <ref>    # e.g. linux_amd64 v2.0.19
set -euo pipefail
cd "$(dirname "$0")/.."

[ $# -eq 2 ] || { echo "usage: $0 <platform> <ref>" >&2; exit 2; }
platform=$1
ref=${2//\//-}  # a branch name (workflow_dispatch) may hold slashes
case "$platform" in
  linux_*) ext=so ;;
  macos_*) ext=dylib ;;
  windows_*) ext=dll ;;
  *) echo "unknown platform $platform" >&2; exit 2 ;;
esac

pkg="libadbc_driver_gizmosql-$ref-$platform"
rm -rf "dist/$pkg" "dist/$pkg.tar.gz"
mkdir -p "dist/$pkg"
cp "go/build/libadbc_driver_gizmosql.$ext" packaging/gizmosql.toml.in LICENSE NOTICE "dist/$pkg/"
tar -C dist -czf "dist/$pkg.tar.gz" "$pkg"
echo "dist/$pkg.tar.gz"
