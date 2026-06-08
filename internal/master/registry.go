package master

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
	"wasmcat/internal/shared"
)

const DefaultWorkerStaleTimeout = 30 * time.Second

// Registry maintains the state of all active workers in the cluster
type Registry struct {
	workers map[string]shared.WorkerNode
	// To handle concurrent access to the registry
	mu sync.RWMutex
	// How long a worker can miss heartbeat updates before the master removes it.
	// This is separate from cleanup interval: interval decides when we scan, timeout decides what is stale.
	workerStaleTimeout time.Duration
}

func NewRegistry() *Registry {
	return NewRegistryWithStaleTimeout(DefaultWorkerStaleTimeout)
}

func NewRegistryWithStaleTimeout(workerStaleTimeout time.Duration) *Registry {
	if workerStaleTimeout <= 0 {
		workerStaleTimeout = DefaultWorkerStaleTimeout
	}

	return &Registry{
		workers:            make(map[string]shared.WorkerNode),
		workerStaleTimeout: workerStaleTimeout,
		// make() initializes the map to avoid nil map errors
		// new() return a pointer to an empty struct
	}
}

func (r *Registry) WorkerStaleTimeout() time.Duration {
	return r.workerStaleTimeout
}

func (r *Registry) RegisterWorker(worker shared.WorkerNode) {
	r.mu.Lock()
	// Unlock memory after the function returns, ensuring it happens even if there's an error -> prevents deadlocks
	defer r.mu.Unlock()
	if worker.LastSeen.IsZero() {
		worker.LastSeen = time.Now()
	}
	if worker.State == "" {
		worker.State = shared.WorkerStateReady
	}
	r.workers[worker.ID] = worker
	slog.Info("worker registered", "worker_id", worker.ID, "address", worker.IPAddress, "state", worker.State)
}

// Take the incoming worker, save to map with key=worker.ID
func RegisterNode(registry *Registry, worker shared.WorkerNode) {
	registry.RegisterWorker(worker)
}

// Look up the worker by ID, update its CPU and RAM info, and update the last seen timestamp
func (r *Registry) UpdateWorkerStatus(heartbeat shared.Heartbeat) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	worker, exists := r.workers[heartbeat.NodeID]
	if !exists {
		return fmt.Errorf("worker with ID %s not found in registry", heartbeat.NodeID)
	}
	worker.CPUFree = heartbeat.CPUFree
	worker.RAMFreeMB = heartbeat.RAMFreeMB
	worker.LastSeen = time.Now()
	if worker.State == "" {
		worker.State = shared.WorkerStateReady
	}
	r.workers[heartbeat.NodeID] = worker
	return nil
}

// GetActiveWorkers returns a slice of all currently active workers in the registry
func (r *Registry) GetActiveWorkers() []shared.WorkerNode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	activeWorkers := make([]shared.WorkerNode, 0, len(r.workers))
	for _, worker := range r.workers {
		activeWorkers = append(activeWorkers, worker)
	}
	return activeWorkers
}

func (r *Registry) GetSchedulableWorkers() []shared.WorkerNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workers := make([]shared.WorkerNode, 0, len(r.workers))
	for _, worker := range r.workers {
		if worker.State == "" || worker.State == shared.WorkerStateReady {
			workers = append(workers, worker)
		}
	}

	return workers
}

func (r *Registry) DrainWorker(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	worker, exists := r.workers[nodeID]
	if !exists {
		return fmt.Errorf("worker with ID %s not found in registry", nodeID)
	}

	worker.State = shared.WorkerStateDraining
	worker.LastSeen = time.Now()
	r.workers[nodeID] = worker
	slog.Info("worker marked draining", "worker_id", nodeID)
	return nil
}

func (r *Registry) ActiveWorkerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.workers)
}

func (r *Registry) WorkerStateCounts() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counts := make(map[string]int)
	for _, worker := range r.workers {
		state := worker.State
		if state == "" {
			state = shared.WorkerStateReady
		}
		counts[state]++
	}

	return counts
}

func (r *Registry) OldestHeartbeatAge(now time.Time) *int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var oldest *time.Time
	for _, worker := range r.workers {
		lastSeen := worker.LastSeen
		if lastSeen.IsZero() {
			continue
		}
		if oldest == nil || lastSeen.Before(*oldest) {
			oldest = &lastSeen
		}
	}
	if oldest == nil {
		return nil
	}

	age := int64(now.Sub(*oldest).Seconds())
	if age < 0 {
		age = 0
	}

	return &age
}

// Clean up workers that haven't sent a heartbeat within the configured stale timeout.
func (r *Registry) Cleanup() {
	r.CleanupAt(time.Now())
}

func (r *Registry) CleanupAt(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, node := range r.workers {
		if now.Sub(node.LastSeen) > r.workerStaleTimeout {
			delete(r.workers, id)
			slog.Info(
				"worker removed after missed heartbeat",
				"worker_id", id,
				"last_seen", node.LastSeen,
				"stale_timeout", r.workerStaleTimeout.String(),
			)
		}
	}
}
