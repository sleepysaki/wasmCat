# Worker Telemetry And Scheduling Plan

## Purpose

wasmCat already has native installation, mTLS communication, worker registration, WASM execution limits, and release automation. The next orchestration gap is scheduling quality: the master should not send work to a node only because it is geographically closest if that node is overloaded.

This plan adds two production inputs to scheduling:

- configured worker location
- live worker capacity from heartbeat telemetry

## Design Goals

- Keep deployment simple: all new inputs are environment variables or init flags.
- Avoid another runtime service: host metrics are read inside the worker process.
- Keep scheduling deterministic and testable: capacity filtering is separate from distance selection.
- Preserve existing behavior when thresholds are left at defaults.
- Keep the telemetry collector swappable through an interface so tests do not depend on host CPU/RAM values.

## Worker Location

Workers now expose static location through:

```text
WORKER_LATITUDE
WORKER_LONGITUDE
```

These values are sent in the worker registration payload. The master stores them in the registry and uses them for Haversine distance calculation.

Latitude must be between `-90` and `90`. Longitude must be between `-180` and `180`. Invalid values fail during config loading or `wasmcat-worker init`.

## Worker Metrics

Worker heartbeats now use a `MetricsProvider` abstraction:

```go
type MetricsProvider interface {
    Snapshot(ctx context.Context) (NodeMetrics, error)
}
```

The production provider reads:

- free CPU percentage from `gopsutil/cpu`
- available memory from `gopsutil/mem`

Heartbeat payloads continue to use the existing shared model:

```json
{
  "node_id": "worker-us-01",
  "cpu_free": 72.4,
  "ram_free_mb": 8192
}
```

## Capacity-Aware Scheduling

The master now supports:

```text
MIN_WORKER_CPU_FREE
MIN_WORKER_RAM_FREE_MB
```

Scheduling performs two stages:

1. Filter active workers below CPU/RAM thresholds.
2. Select the geographically closest worker among the remaining workers.

Default thresholds are `0`, which keeps all workers eligible and preserves current behavior unless operators opt in.

## Failure Behavior

If all workers are below capacity thresholds, the scheduler returns:

```text
no active workers meet capacity requirements
```

The dispatcher bubbles that up as a dispatch failure, and the master returns a `503` JSON error to the caller.

## Operational Guidance

- Start with conservative thresholds such as `MIN_WORKER_CPU_FREE=10` and `MIN_WORKER_RAM_FREE_MB=256`.
- Set worker coordinates explicitly; leaving both at zero makes every unset worker appear near the Gulf of Guinea.
- Keep `WORKER_ADVERTISE_ADDRESS` reachable from the master; location only affects selection, not connectivity.
- Monitor heartbeat logs after rollout because missing certs or unreachable master URLs stop telemetry before metrics are sent.
