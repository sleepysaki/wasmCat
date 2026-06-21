package master_test

import (
	"testing"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestRegistryCleanupUsesConfiguredStaleTimeout(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	registry := master.NewRegistryWithStaleTimeout(2 * time.Minute)

	registry.RegisterWorker(shared.WorkerNode{
		ID:       "fresh-worker",
		LastSeen: now.Add(-90 * time.Second),
	})
	registry.RegisterWorker(shared.WorkerNode{
		ID:       "stale-worker",
		LastSeen: now.Add(-3 * time.Minute),
	})

	registry.CleanupAt(now)

	workers := registry.GetActiveWorkers()
	if len(workers) != 1 {
		t.Fatalf("expected one active worker after cleanup, got %d", len(workers))
	}
	if workers[0].ID != "fresh-worker" {
		t.Fatalf("expected fresh-worker to remain, got %q", workers[0].ID)
	}
}

func TestRegistryDefaultsInvalidStaleTimeout(t *testing.T) {
	registry := master.NewRegistryWithStaleTimeout(0)

	if registry.WorkerStaleTimeout() != master.DefaultWorkerStaleTimeout {
		t.Fatalf("expected default stale timeout %s, got %s", master.DefaultWorkerStaleTimeout, registry.WorkerStaleTimeout())
	}
}
