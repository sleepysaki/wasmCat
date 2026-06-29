#!/usr/bin/env sh
# install-or-update.sh installs or updates wasmCat native binaries on a Linux host
# (tested on Ubuntu 24.04) without Docker or any container runtime.
#
# It is safe to re-run: it backs up the existing binary as <binary>.bak, installs
# the new one, and restarts only the relevant systemd service. Host configuration
# under the config directory (/etc/wasmcat by default) is never touched, so
# *.env files, the certs directory, and the SQLite job database are preserved.
#
# Run on the master VM:    sudo ./install-or-update.sh --role master --source ./dist
# Run on a worker VM:      sudo ./install-or-update.sh --role worker --source ./dist
# Run on an operator box:  ./install-or-update.sh --role ctl,ui --source ./dist
# Download a release:      sudo ./install-or-update.sh --role master --version v0.1.0 --repo owner/repo
# Roll back a bad update:  sudo ./install-or-update.sh --role master --rollback
#
# See docs/INSTALLATION.md for the full deployment narrative.

set -eu

ROLE=""
SOURCE_DIR="./dist"
RELEASE_VERSION=""
REPO="${WASMCAT_REPO:-}"
ARCH=""
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/wasmcat"
STATE_DIR="/var/lib/wasmcat"
SERVICE_USER="wasmcat"
CERTS_FROM=""
INSTALL_SERVICE=0
NO_RESTART=0
ROLLBACK=0
DRY_RUN=0

DOWNLOAD_TMP=""

log() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    printf 'dry-run: %s\n' "$*"
    return 0
  fi
  "$@"
}

cleanup() {
  if [ -n "$DOWNLOAD_TMP" ] && [ -d "$DOWNLOAD_TMP" ]; then
    rm -rf "$DOWNLOAD_TMP"
  fi
}
trap cleanup EXIT INT TERM

usage() {
  cat <<'EOF'
Usage: install-or-update.sh --role LIST [options]

Role (required; positional or flag):
  install-or-update.sh master            Positional shorthand for --role master.
  --role LIST           Comma-separated components: master,worker,ctl,ui (or 'all').

Source of binaries (choose one; defaults to --source ./dist):
  --source DIR          Directory containing prebuilt binaries named
                        <component>-linux-<arch> (the output of scripts/build.sh).
  --version VER         Download release VER from GitHub instead of --source.
  --repo OWNER/REPO     GitHub repository for --version downloads
                        (or set WASMCAT_REPO).

Options:
  --arch ARCH           Target architecture: amd64 or arm64 (default: autodetect).
  --bin-dir DIR         Install directory (default: /usr/local/bin).
  --config-dir DIR      Config directory; env/certs/DB here are preserved
                        (default: /etc/wasmcat).
  --certs-from DIR      Copy a cert bundle (from gen-certs.sh) into
                        <config-dir>/certs with correct ownership and per-file
                        permissions. Existing certs are overwritten.
  --install-service     Also install/refresh the systemd unit file(s) from source
                        and run daemon-reload.
  --no-restart          Install binaries but do not restart any service.
  --rollback            Restore the previous <binary>.bak for --role and restart.
  --dry-run             Print the actions without changing anything.
  -h, --help            Show this help.

Components map to: binary name / systemd service / version command
  master  wasmcat-master  wasmcat-master  "wasmcat-master version"
  worker  wasmcat-worker  wasmcat-worker  "wasmcat-worker version"
  ctl     wasmcatctl      (none)          "wasmcatctl version"
  ui      wasmcat-ui      (none)          "wasmcat-ui --version"
EOF
}

# Allow a leading positional role: install-or-update.sh master --source ./dist
if [ $# -gt 0 ]; then
  case "$1" in
    -*) : ;;
    *) ROLE="$1"; shift ;;
  esac
fi

while [ $# -gt 0 ]; do
  case "$1" in
    --role) ROLE="${2:-}"; shift 2 ;;
    --role=*) ROLE="${1#*=}"; shift ;;
    --source) SOURCE_DIR="${2:-}"; shift 2 ;;
    --source=*) SOURCE_DIR="${1#*=}"; shift ;;
    --version) RELEASE_VERSION="${2:-}"; shift 2 ;;
    --version=*) RELEASE_VERSION="${1#*=}"; shift ;;
    --repo) REPO="${2:-}"; shift 2 ;;
    --repo=*) REPO="${1#*=}"; shift ;;
    --arch) ARCH="${2:-}"; shift 2 ;;
    --arch=*) ARCH="${1#*=}"; shift ;;
    --bin-dir) BIN_DIR="${2:-}"; shift 2 ;;
    --bin-dir=*) BIN_DIR="${1#*=}"; shift ;;
    --config-dir) CONFIG_DIR="${2:-}"; shift 2 ;;
    --config-dir=*) CONFIG_DIR="${1#*=}"; shift ;;
    --certs-from) CERTS_FROM="${2:-}"; shift 2 ;;
    --certs-from=*) CERTS_FROM="${1#*=}"; shift ;;
    --install-service) INSTALL_SERVICE=1; shift ;;
    --no-restart) NO_RESTART=1; shift ;;
    --rollback) ROLLBACK=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (use --help)" ;;
  esac
done

[ -n "$ROLE" ] || { usage; die "--role is required"; }

# Expand role list into the canonical component set.
if [ "$ROLE" = "all" ]; then
  COMPONENTS="master worker ctl ui"
else
  COMPONENTS="$(printf '%s' "$ROLE" | tr ',' ' ')"
fi
for component in $COMPONENTS; do
  case "$component" in
    master|worker|ctl|ui) ;;
    *) die "unknown component: $component" ;;
  esac
done

detect_arch() {
  if [ -n "$ARCH" ]; then
    return
  fi
  machine="$(uname -m)"
  case "$machine" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) die "unsupported architecture: $machine (use --arch)" ;;
  esac
}

binary_name() {
  case "$1" in
    master) echo "wasmcat-master" ;;
    worker) echo "wasmcat-worker" ;;
    ctl) echo "wasmcatctl" ;;
    ui) echo "wasmcat-ui" ;;
  esac
}

service_name() {
  # Only master and worker ship a systemd unit; ctl and ui run on demand.
  case "$1" in
    master) echo "wasmcat-master" ;;
    worker) echo "wasmcat-worker" ;;
    *) echo "" ;;
  esac
}

# read_version runs the installed/new binary's version command and prints the
# raw version token, or "unknown" if the binary is absent or does not respond.
read_version() {
  component="$1"
  path="$2"
  [ -x "$path" ] || { echo "unknown"; return; }
  case "$component" in
    ui) out="$("$path" --version 2>/dev/null || true)" ;;
    *) out="$("$path" version 2>/dev/null || true)" ;;
  esac
  # Output looks like "wasmcat-master 1.2.3"; keep the last field.
  token="$(printf '%s' "$out" | awk '{print $NF}')"
  [ -n "$token" ] && echo "$token" || echo "unknown"
}

need_root() {
  if [ "$DRY_RUN" -eq 1 ]; then
    return
  fi
  if [ "$(id -u)" -ne 0 ]; then
    die "writing to $BIN_DIR requires root; re-run with sudo"
  fi
}

components_include() {
  for component in $COMPONENTS; do
    [ "$component" = "$1" ] && return 0
  done
  return 1
}

is_node_role() {
  components_include master || components_include worker
}

# ensure_service_user creates the dedicated wasmcat system account before any
# chown, so installs do not fail with "chown: invalid user: 'wasmcat:wasmcat'".
ensure_service_user() {
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    return 0
  fi
  log "creating service user $SERVICE_USER"
  run useradd --system --home "$STATE_DIR" --shell /usr/sbin/nologin "$SERVICE_USER" || true
}

ensure_dirs() {
  run mkdir -p "$CONFIG_DIR" "$STATE_DIR"
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    run chown "$SERVICE_USER:$SERVICE_USER" "$CONFIG_DIR" "$STATE_DIR"
  fi
}

# install_certs copies a gen-certs.sh bundle into <config-dir>/certs using
# explicit per-file permissions instead of fragile "chmod *.crt" wildcards that
# fail when a file is missing.
install_certs() {
  [ -d "$CERTS_FROM" ] || die "--certs-from $CERTS_FROM is not a directory"
  dst="$CONFIG_DIR/certs"
  run mkdir -p "$dst"

  copied=0
  for path in "$CERTS_FROM"/*; do
    [ -f "$path" ] || continue
    base="$(basename "$path")"
    case "$base" in
      *.crt) run install -m 0640 "$path" "$dst/$base"; copied=1 ;;
      *.key) run install -m 0600 "$path" "$dst/$base"; copied=1 ;;
      *) : ;;
    esac
  done
  [ "$copied" -eq 1 ] || warn "no .crt/.key files found in $CERTS_FROM"

  if id "$SERVICE_USER" >/dev/null 2>&1; then
    run chown -R "$SERVICE_USER:$SERVICE_USER" "$dst"
  fi
  run chmod 0750 "$dst"
  log "installed certs into $dst with explicit per-file permissions"
}

download_release() {
  [ -n "$REPO" ] || die "--version requires --repo OWNER/REPO (or WASMCAT_REPO)"
  command -v curl >/dev/null 2>&1 || die "curl is required to download releases"
  DOWNLOAD_TMP="$(mktemp -d)"
  base="https://github.com/$REPO/releases/download/$RELEASE_VERSION"
  log "downloading wasmCat $RELEASE_VERSION from $REPO ($ARCH)"

  for component in $COMPONENTS; do
    file="$(binary_name "$component")-linux-$ARCH"
    log "  fetching $file"
    run curl -fSL -o "$DOWNLOAD_TMP/$file" "$base/$file"
  done
  if [ "$INSTALL_SERVICE" -eq 1 ]; then
    for component in $COMPONENTS; do
      svc="$(service_name "$component")"
      [ -n "$svc" ] || continue
      run curl -fSL -o "$DOWNLOAD_TMP/$svc.service" "$base/$svc.service"
    done
  fi

  # checksums.txt is best-effort: verify what we downloaded if it is published.
  if curl -fSL -o "$DOWNLOAD_TMP/checksums.txt" "$base/checksums.txt" 2>/dev/null; then
    if command -v sha256sum >/dev/null 2>&1; then
      log "verifying checksums"
      ( cd "$DOWNLOAD_TMP" && run sha256sum -c checksums.txt --ignore-missing ) \
        || die "checksum verification failed"
    else
      warn "sha256sum not found; skipping checksum verification"
    fi
  else
    warn "checksums.txt not published for $RELEASE_VERSION; skipping verification"
  fi

  SOURCE_DIR="$DOWNLOAD_TMP"
}

install_component() {
  component="$1"
  bin="$(binary_name "$component")"
  src="$SOURCE_DIR/$bin-linux-$ARCH"
  dst="$BIN_DIR/$bin"

  [ -f "$src" ] || die "missing binary: $src (build with scripts/build.sh or use --version)"

  current="$(read_version "$component" "$dst")"
  incoming="$(read_version "$component" "$src")"
  log "[$component] installed=$current -> new=$incoming"

  if [ -f "$dst" ]; then
    run cp -p "$dst" "$dst.bak"
  fi
  run install -m 0755 "$src" "$dst"

  if [ "$INSTALL_SERVICE" -eq 1 ]; then
    svc="$(service_name "$component")"
    if [ -n "$svc" ]; then
      unit_src="$SOURCE_DIR/$svc.service"
      if [ -f "$unit_src" ]; then
        run install -m 0644 "$unit_src" "/etc/systemd/system/$svc.service"
        run systemctl daemon-reload
      else
        warn "[$component] --install-service set but $unit_src not found; skipping unit"
      fi
    fi
  fi

  restart_service "$component"
}

# restart_service restarts only the unit for this component, and auto-rolls back
# that component if the service does not come back active.
restart_service() {
  component="$1"
  svc="$(service_name "$component")"
  [ -n "$svc" ] || return 0

  if [ "$NO_RESTART" -eq 1 ]; then
    log "[$component] --no-restart set; restart $svc manually"
    return 0
  fi
  if ! command -v systemctl >/dev/null 2>&1; then
    warn "[$component] systemctl not found; start $svc manually"
    return 0
  fi
  # Don't try to start a service the operator has not enabled/configured yet
  # (first-time install before init/certs). Only restart if the unit exists.
  if ! systemctl cat "$svc.service" >/dev/null 2>&1; then
    log "[$component] $svc not installed; configure it then 'systemctl enable --now $svc'"
    return 0
  fi

  log "[$component] restarting $svc"
  run systemctl restart "$svc"

  if [ "$DRY_RUN" -eq 1 ]; then
    return 0
  fi
  if systemctl is-active --quiet "$svc"; then
    log "[$component] $svc is active"
  else
    warn "[$component] $svc failed to start; rolling back"
    rollback_component "$component"
    die "[$component] update rolled back after failed restart; check 'journalctl -u $svc'"
  fi
}

rollback_component() {
  component="$1"
  bin="$(binary_name "$component")"
  dst="$BIN_DIR/$bin"
  if [ ! -f "$dst.bak" ]; then
    warn "[$component] no $dst.bak to roll back to"
    return 1
  fi
  log "[$component] restoring $dst from .bak"
  run cp -p "$dst.bak" "$dst"
  svc="$(service_name "$component")"
  if [ -n "$svc" ] && [ "$NO_RESTART" -eq 0 ] && command -v systemctl >/dev/null 2>&1; then
    run systemctl restart "$svc" || warn "[$component] restart after rollback failed"
  fi
}

main() {
  detect_arch
  need_root

  if [ "$ROLLBACK" -eq 1 ]; then
    log "rolling back: $COMPONENTS"
    for component in $COMPONENTS; do
      rollback_component "$component" || true
    done
    log "rollback complete"
    return 0
  fi

  if [ -n "$RELEASE_VERSION" ]; then
    download_release
  fi

  # A node role needs the service account and directories in place before any
  # chown/cert copy. Operator-only installs (ctl/ui) skip this.
  if is_node_role; then
    ensure_service_user
    ensure_dirs
  fi
  if [ -n "$CERTS_FROM" ]; then
    install_certs
  fi

  log "preserving $CONFIG_DIR/*.env, $CONFIG_DIR/certs, and the SQLite job database"
  log "installing: $COMPONENTS (arch=$ARCH, bin-dir=$BIN_DIR)"
  for component in $COMPONENTS; do
    install_component "$component"
  done
  log "done"
}

main
