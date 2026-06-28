#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DIST_DIR="${DIST_DIR:-"$ROOT_DIR/dist"}"
VERSION="${VERSION:-dev}"

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
