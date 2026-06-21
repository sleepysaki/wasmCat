package master_test

import (
	"strings"
	"testing"
	"wasmcat/internal/master"
	"wasmcat/internal/shared"
)

func TestSchedulerSelectsClosestWorkerThatMeetsCapacity(t *testing.T) {
	scheduler := &master.Scheduler{
		MinCPUFree:   20,
		MinRAMFreeMB: 512,
	}

	worker, err := scheduler.SelectWorker(0, 0, []shared.WorkerNode{
		{
			ID:        "near-overloaded",
			Latitude:  0,
			Longitude: 1,
			CPUFree:   5,
			RAMFreeMB: 4096,
		},
		{
			ID:        "far-eligible",
			Latitude:  10,
			Longitude: 10,
			CPUFree:   80,
			RAMFreeMB: 4096,
		},
		{
			ID:        "near-eligible",
			Latitude:  0,
			Longitude: 2,
			CPUFree:   40,
			RAMFreeMB: 1024,
		},
	})
	if err != nil {
		t.Fatalf("SelectWorker returned error: %v", err)
	}

	if worker.ID != "near-eligible" {
		t.Fatalf("expected near-eligible, got %q", worker.ID)
	}
}

func TestSchedulerRejectsWorkersBelowCapacity(t *testing.T) {
	scheduler := &master.Scheduler{
		MinCPUFree:   50,
		MinRAMFreeMB: 2048,
	}

	_, err := scheduler.SelectWorker(0, 0, []shared.WorkerNode{
		{
			ID:        "low-cpu",
			CPUFree:   10,
			RAMFreeMB: 4096,
		},
		{
			ID:        "low-ram",
			CPUFree:   90,
			RAMFreeMB: 512,
		},
	})
	if err == nil {
		t.Fatal("expected capacity error")
	}
	if !strings.Contains(err.Error(), "capacity requirements") {
		t.Fatalf("expected capacity error, got %v", err)
	}
}

func TestFilterWorkersByCapacityAllowsZeroThresholds(t *testing.T) {
	workers := []shared.WorkerNode{
		{ID: "a", CPUFree: 0, RAMFreeMB: 0},
		{ID: "b", CPUFree: 10, RAMFreeMB: 128},
	}

	eligible := master.FilterWorkersByCapacity(workers, 0, 0)
	if len(eligible) != 2 {
		t.Fatalf("expected all workers to be eligible, got %d", len(eligible))
	}
}

func TestSchedulerSkipsDrainingWorkers(t *testing.T) {
	scheduler := &master.Scheduler{}

	worker, err := scheduler.SelectWorker(0, 0, []shared.WorkerNode{
		{ID: "draining-near", Latitude: 0, Longitude: 0, CPUFree: 100, RAMFreeMB: 4096, State: shared.WorkerStateDraining},
		{ID: "ready-far", Latitude: 10, Longitude: 10, CPUFree: 100, RAMFreeMB: 4096, State: shared.WorkerStateReady},
	})
	if err != nil {
		t.Fatalf("SelectWorker returned error: %v", err)
	}
	if worker.ID != "ready-far" {
		t.Fatalf("expected ready worker to be selected, got %+v", worker)
	}
}
