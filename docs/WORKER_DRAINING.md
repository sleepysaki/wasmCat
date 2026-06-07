# Worker Draining

Worker draining lets a node stop receiving new executions without immediately disappearing from the master registry. This is useful before restart, upgrade, host maintenance, or controlled shutdown.

## States

Workers currently use these states:

| State | Meaning |
| --- | --- |
| `ready` | Worker can receive new executions. |
| `draining` | Worker remains registered but is excluded from scheduling. |

Workers without an explicit state are treated as `ready` for backward compatibility.

## Drain Endpoint

```text
POST /internal/drain
```

Request body:

```json
{
  "node_id": "worker-us-01"
}
```

The endpoint is protected by the same mTLS and certificate identity validation as registration and heartbeat. A worker can only drain the ID that matches its certificate identity.

## Scheduler Behavior

The dispatcher reads schedulable workers from the registry. Draining workers are excluded before CPU/RAM capacity filtering and distance selection. A draining worker can finish any work it already accepted, but it will not receive new dispatches.

## Worker Shutdown

When the worker telemetry context is cancelled, telemetry sends one best-effort drain request before exiting. This gives the master an immediate signal during normal process shutdown instead of waiting for registry heartbeat cleanup.

## Metrics

Master metrics include `workers_by_state`, so operators can see ready and draining worker counts through:

```text
GET /wasmcat/metrics
```
