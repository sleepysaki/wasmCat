package shared

import (
	"strconv"
	"sync"
	"time"
)

type Metrics struct {
	mu sync.RWMutex

	startedAt time.Time

	requestsTotal    uint64
	requestsByStatus map[string]uint64
	requestsByPath   map[string]uint64

	dispatchSuccess uint64
	dispatchFailure uint64

	workerExecutionSuccess uint64
	workerExecutionFailure uint64
}

type MetricsResponse struct {
	Role             string            `json:"role"`
	NodeID           string            `json:"node_id,omitempty"`
	UptimeSeconds    int64             `json:"uptime_seconds"`
	RequestsTotal    uint64            `json:"requests_total"`
	RequestsByStatus map[string]uint64 `json:"requests_by_status"`
	RequestsByPath   map[string]uint64 `json:"requests_by_path"`
	Master           *MasterMetrics    `json:"master,omitempty"`
	Worker           *WorkerMetrics    `json:"worker,omitempty"`
}

type MasterMetrics struct {
	ActiveWorkers    int            `json:"active_workers"`
	WorkersByState   map[string]int `json:"workers_by_state"`
	OldestHeartbeatS *int64         `json:"oldest_heartbeat_seconds,omitempty"`
	DispatchSuccess  uint64         `json:"dispatch_success"`
	DispatchFailure  uint64         `json:"dispatch_failure"`
}

type WorkerMetrics struct {
	ExecutionSuccess uint64           `json:"execution_success"`
	ExecutionFailure uint64           `json:"execution_failure"`
	Cache            ModuleCacheStats `json:"cache"`
}

type ModuleCacheStats struct {
	Entries    int   `json:"entries"`
	Bytes      int64 `json:"bytes"`
	MaxEntries int   `json:"max_entries"`
	MaxBytes   int64 `json:"max_bytes"`
}

func NewMetrics() *Metrics {
	return &Metrics{
		startedAt:        time.Now(),
		requestsByStatus: make(map[string]uint64),
		requestsByPath:   make(map[string]uint64),
	}
}

func (m *Metrics) ObserveRequest(path string, status int) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestsTotal++
	m.requestsByStatus[strconv.Itoa(status)]++
	m.requestsByPath[path]++
}

func (m *Metrics) IncDispatchSuccess() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatchSuccess++
}

func (m *Metrics) IncDispatchFailure() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatchFailure++
}

func (m *Metrics) IncWorkerExecutionSuccess() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.workerExecutionSuccess++
}

func (m *Metrics) IncWorkerExecutionFailure() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.workerExecutionFailure++
}

func (m *Metrics) SnapshotBase(role string, nodeID string) MetricsResponse {
	if m == nil {
		m = NewMetrics()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsResponse{
		Role:             role,
		NodeID:           nodeID,
		UptimeSeconds:    int64(time.Since(m.startedAt).Seconds()),
		RequestsTotal:    m.requestsTotal,
		RequestsByStatus: copyUint64Map(m.requestsByStatus),
		RequestsByPath:   copyUint64Map(m.requestsByPath),
	}
}

func (m *Metrics) MasterSnapshot(activeWorkers int, workersByState map[string]int, oldestHeartbeatSeconds *int64) MetricsResponse {
	response := m.SnapshotBase("master", "")

	m.mu.RLock()
	defer m.mu.RUnlock()
	response.Master = &MasterMetrics{
		ActiveWorkers:    activeWorkers,
		WorkersByState:   workersByState,
		OldestHeartbeatS: oldestHeartbeatSeconds,
		DispatchSuccess:  m.dispatchSuccess,
		DispatchFailure:  m.dispatchFailure,
	}

	return response
}

func (m *Metrics) WorkerSnapshot(nodeID string, cache ModuleCacheStats) MetricsResponse {
	response := m.SnapshotBase("worker", nodeID)

	m.mu.RLock()
	defer m.mu.RUnlock()
	response.Worker = &WorkerMetrics{
		ExecutionSuccess: m.workerExecutionSuccess,
		ExecutionFailure: m.workerExecutionFailure,
		Cache:            cache,
	}

	return response
}

func copyUint64Map(source map[string]uint64) map[string]uint64 {
	copied := make(map[string]uint64, len(source))
	for key, value := range source {
		copied[key] = value
	}

	return copied
}
