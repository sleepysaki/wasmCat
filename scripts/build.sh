#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DIST_DIR="${DIST_DIR:-"$ROOT_DIR/dist"}"
VERSION="${VERSION:-dev}"

# require_go fails early with a clear message when the local toolchain is older
# than the version in go.mod. Old Go reports errors like
# "invalid go version '1.26.2': must match format 1.23", which is confusing on a VM.
require_go() {
  if ! command -v go >/dev/null 2>&1; then
    echo "error: go toolchain not found." >&2
    echo "Install the Go version in go.mod, or download prebuilt release binaries instead of building on this host." >&2
    echo "See docs/INSTALLATION.md (Install or Update With the Script)." >&2
    exit 1
  fi

  required="$(awk '/^go /{print $2; exit}' "$ROOT_DIR/go.mod")"
  installed="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
  req_num="$(printf '%s' "$required" | awk -F. '{printf "%d%03d", $1, $2}')"
  inst_num="$(printf '%s' "$installed" | awk -F. '{printf "%d%03d", $1, $2}')"

  if [ -n "$req_num" ] && [ -n "$inst_num" ] && [ "$inst_num" -lt "$req_num" ]; then
    echo "error: Go $installed is older than the $required required by go.mod." >&2
    echo "Upgrade Go to >= $required, or build on another machine and copy the binaries." >&2
    echo "For production, prefer release binaries (see docs/INSTALLATION.md)." >&2
    exit 1
  fi
}

require_go

mkdir -p "$DIST_DIR"

build_binary() {
  name="$1"
  package="$2"
  target_os="$3"
  target_arch="$4"

  extension=""
  if [ "$target_os" = "windows" ]; then
    extension=".exe"
  fi

  output="$DIST_DIR/${name}-${target_os}-${target_arch}${extension}"

  echo "building $output"
  CGO_ENABLED=0 \
    GOOS="$target_os" \
    GOARCH="$target_arch" \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=$VERSION" \
      -o "$output" \
      "$package"
}

build_binary "wasmcat-master" "./cmd/master" "linux" "amd64"
build_binary "wasmcat-worker" "./cmd/worker" "linux" "amd64"
build_binary "wasmcatctl" "./cmd/wasmcatctl" "linux" "amd64"
build_binary "wasmcat-ui" "./cmd/wasmcat-ui" "linux" "amd64"
build_binary "wasmcat-master" "./cmd/master" "windows" "amd64"
build_binary "wasmcat-worker" "./cmd/worker" "windows" "amd64"
build_binary "wasmcatctl" "./cmd/wasmcatctl" "windows" "amd64"
build_binary "wasmcat-ui" "./cmd/wasmcat-ui" "windows" "amd64"

cp "$ROOT_DIR"/packaging/systemd/wasmcat-* "$DIST_DIR"/

if command -v sha256sum >/dev/null 2>&1; then
  (
    cd "$DIST_DIR"
    rm -f checksums.txt
    sha256sum * > checksums.txt
  )
elif command -v shasum >/dev/null 2>&1; then
  (
    cd "$DIST_DIR"
    rm -f checksums.txt
    shasum -a 256 * > checksums.txt
  )
else
  echo "sha256 tool not found; skipping checksums"
fi

echo "release artifacts written to $DIST_DIR"
