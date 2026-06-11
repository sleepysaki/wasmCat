package master_test

import (
	"testing"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestExecutionRequestTrackerEvictsLeastRecentCompletedEntry(t *testing.T) {
	tracker := master.NewExecutionRequestTrackerWithLimit(time.Minute, 2)

	req1 := shared.ExecutionRequest{RequestID: "req-one", ModuleName: "echo", ModuleURL: "http://module.test/one.wasm"}
	req2 := shared.ExecutionRequest{RequestID: "req-two", ModuleName: "echo", ModuleURL: "http://module.test/two.wasm"}
	req3 := shared.ExecutionRequest{RequestID: "req-three", ModuleName: "echo", ModuleURL: "http://module.test/three.wasm"}

	mustBegin(t, tracker, req1, master.ExecutionRequestStarted)
	tracker.Complete(req1.RequestID, shared.ExecutionResponse{RequestID: req1.RequestID, Result: "one"})
	mustBegin(t, tracker, req2, master.ExecutionRequestStarted)
	tracker.Complete(req2.RequestID, shared.ExecutionResponse{RequestID: req2.RequestID, Result: "two"})

	mustBegin(t, tracker, req1, master.ExecutionRequestCompleted)
	mustBegin(t, tracker, req3, master.ExecutionRequestStarted)
	tracker.Complete(req3.RequestID, shared.ExecutionResponse{RequestID: req3.RequestID, Result: "three"})

	mustBegin(t, tracker, req2, master.ExecutionRequestStarted)
}

func TestExecutionRequestTrackerDoesNotEvictInFlightEntries(t *testing.T) {
	tracker := master.NewExecutionRequestTrackerWithLimit(time.Minute, 1)

	inFlight := shared.ExecutionRequest{RequestID: "req-running", ModuleName: "echo", ModuleURL: "http://module.test/running.wasm"}
	completed := shared.ExecutionRequest{RequestID: "req-done", ModuleName: "echo", ModuleURL: "http://module.test/done.wasm"}

	mustBegin(t, tracker, inFlight, master.ExecutionRequestStarted)
	mustBegin(t, tracker, completed, master.ExecutionRequestStarted)
	tracker.Complete(completed.RequestID, shared.ExecutionResponse{RequestID: completed.RequestID, Result: "done"})

	mustBegin(t, tracker, inFlight, master.ExecutionRequestInFlight)
	mustBegin(t, tracker, completed, master.ExecutionRequestStarted)
}

func mustBegin(t *testing.T, tracker *master.ExecutionRequestTracker, req shared.ExecutionRequest, expected master.ExecutionRequestDecision) {
	t.Helper()

	status, err := tracker.Begin(req)
	if err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}
	if status.Decision != expected {
		t.Fatalf("expected decision %d for %s, got %d", expected, req.RequestID, status.Decision)
	}
}
