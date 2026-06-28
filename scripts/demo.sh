#!/usr/bin/env sh
# demo.sh runs a guided, end-to-end check of every core wasmCat capability and
# prints a requirement-by-requirement result map. It is the quickest reliable
# way to demonstrate that the orchestrator works: it builds the native binaries
# and then drives the real master gateway, dispatcher, scheduler, wazero engine,
# durable job store, and mTLS stack through the test harness.
#
# Usage:
#   sh scripts/demo.sh            # summary per capability
#   VERBOSE=1 sh scripts/demo.sh  # show full test logs (real dispatch/execute output)
#
# For a live multi-VM walkthrough, see docs/PRODUCTION_VM_DEPLOYMENT.md.

set -u

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT_DIR"

VERBOSE="${VERBOSE:-0}"
PASS=0
FAIL=0
FAILED_GROUPS=""
LOG="/tmp/wasmcat-demo.$$"

hr() { printf '%s\n' "------------------------------------------------------------"; }

# check NAME PACKAGE RUN_REGEX
# Always runs go test with -v so individual checks can be counted; the full log
# is only shown when VERBOSE=1.
check() {
  name="$1"
  pkg="$2"
  regex="$3"

  hr
  printf '> %s\n' "$name"
  if go test -count=1 -v "$pkg" -run "$regex" >"$LOG" 2>&1; then
    count="$(grep -c -- '--- PASS:' "$LOG")"
    printf '  PASS (%s checks)\n' "$count"
    PASS=$((PASS + 1))
  else
    printf '  FAIL\n'
    tail -n 25 "$LOG"
    FAIL=$((FAIL + 1))
    FAILED_GROUPS="$FAILED_GROUPS\n  - $name"
  fi
  if [ "$VERBOSE" = "1" ]; then
    cat "$LOG"
  fi
  rm -f "$LOG"
}

hr
printf 'wasmCat capability demo\n'
hr

printf 'Building native binaries (master, worker, wasmcatctl, ui)...\n'
if ! go build ./... ; then
  printf 'build failed; cannot run demo\n' >&2
  exit 1
fi
printf 'build ok\n'

# Each line maps a core functional requirement to the tests that prove it.
check "Setup: init config, dev certs, versions, loopback warnings" ./tests/bootstrap "Init|Version"
check "Worker registration, heartbeat, and registry state"          ./tests/master   "Register|Heartbeat|ListWorkers|Registry|Workers"
check "Geo-aware distribution + execution regardless of location"   ./tests/integration "GeoAware|DispatchesThroughWorkerEngine"
check "Real binaries: master->worker->wazero over mTLS"             ./tests/smoke    "Binaries|Version"
check "Capacity-aware geo scheduling (nearest, drain, thresholds)"  ./tests/master   "Scheduler|Drain|Capacity"
check "Synchronous execute API and request idempotency"             ./tests/master   "Execute|Idempotency|RequestID|CachedResponse"
check "Durable async jobs: create, query, recovery, completion"     ./tests/master   "Job"
check "mTLS identity and execute-client authorization"              ./tests/master   "Identity|Client|Allowlist"
check "ACR just-in-time module provisioning"                        ./tests/master   "ACR"
check "Worker limits, digest verification, and module cache"        ./tests/worker   "Cache|Limit|Digest"
check "Any-WASM execution: custom ABI + standard WASI (wasip1)"      ./tests/worker   "WASI|AutoDetectsCustomABI"
check "Auto-location with multi-provider fallback"                  ./tests/geolocation "Detect"
check "Health, readiness, and metrics endpoints"                    ./tests/master   "Health|Ready|Metrics"

hr
printf 'Summary: %d capability groups passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failed groups:%b\n' "$FAILED_GROUPS"
  exit 1
fi
printf 'All core capabilities verified. Ready to demo.\n'
hr
