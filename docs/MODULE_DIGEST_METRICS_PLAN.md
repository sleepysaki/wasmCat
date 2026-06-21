# Module Digest Metrics Plan

## Purpose

Worker module digest validation already protects execution by rejecting malformed digests and mismatched downloaded bytes before compilation. The next production step is observability: operators need to see these integrity failures in `/wasmcat/metrics` without searching logs or reconstructing failures from generic execution counts.

## Scope

Add worker-level JSON metrics for two digest failure categories:

- `module_digest_invalid`: the request supplied an unsupported, incomplete, or malformed digest value.
- `module_digest_mismatch`: the digest format was valid, but downloaded WASM bytes did not match the expected SHA-256 value.

These counters complement `execution_failure`; they do not replace it. Every digest failure is still an execution failure, but not every execution failure is a digest failure.

## Execution Plan

1. Extend the shared metrics collector with two worker counters and safe increment methods.
2. Include the counters in `WorkerMetrics` so `GET /wasmcat/metrics` exposes them in the existing JSON response.
3. Increment the correct counter from the worker invoke handler after the engine error is classified.
4. Add tests that call `/invoke` with invalid and mismatched digests, then assert the metrics response reports separate counts.
5. Update `docs/METRICS.md`, `docs/TECHNICAL_REFERENCE.md`, and `docs/TESTING.md`.

## Acceptance Criteria

- Invalid digest requests increase `worker.module_digest_invalid`.
- Mismatched digest requests increase `worker.module_digest_mismatch`.
- Both request types still increase `worker.execution_failure`.
- Successful executions do not increase either digest-failure counter.
- `go test ./...`, `go vet ./...`, `go build ./...`, focused coverage, and release packaging all pass.

