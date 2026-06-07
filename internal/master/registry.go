package master

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
	"wasmcat/internal/shared"
)

// Registry maintains the state of all active workers in the cluster
type Registry struct {
	workers map[string]shared.WorkerNode
	// To handle concurrent access to the registry
	mu sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]shared.WorkerNode),
		// make() initializes the map to avoid nil map errors
		// new() return a pointer to an empty struct
	}
}

func (r *Registry) RegisterWorker(worker shared.WorkerNode) {
	r.mu.Lock()
	// Unlock memory after the function returns, ensuring it happens even if there's an error -> prevents deadlocks
	defer r.mu.Unlock()
	if worker.LastSeen.IsZero() {
		worker.LastSeen = time.Now()
	}
	r.workers[worker.ID] = worker
	slog.Info("worker registered", "worker_id", worker.ID, "address", worker.IPAddress)
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

func (r *Registry) ActiveWorkerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.workers)
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

// Clean up workers that haven't sent a heartbeat in the last 30 seconds
func (r *Registry) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, node := range r.workers {
		if time.Since(node.LastSeen) > 30*time.Second {
			delete(r.workers, id)
			slog.Info("worker removed after missed heartbeat", "worker_id", id, "last_seen", node.LastSeen)
		}
	}
}
