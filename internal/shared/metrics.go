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
	// dispatchReschedules counts times the master had to move an execution request
	// from one selected worker to another after a retryable forwarding failure.
	dispatchReschedules uint64
	// dispatchRescheduleExhausted counts requests where every retryable worker candidate failed.
	dispatchRescheduleExhausted uint64

	requestCacheHits           uint64
	requestInProgressConflicts uint64
	requestIDConflicts         uint64

	workerExecutionSuccess uint64
	workerExecutionFailure uint64
	// workerModuleDigestInvalid counts requests that provided a digest the worker could not validate.
	// This usually points to caller input or manifest-resolution problems rather than bad module storage.
	workerModuleDigestInvalid uint64
	// workerModuleDigestMismatch counts requests where downloaded bytes did not match the declared digest.
	// This is the stronger integrity signal because the remote content differs from the expected module.
	workerModuleDigestMismatch uint64
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
	// DispatchReschedules is an availability signal: it means the dispatcher recovered
	// from one selected-worker failure by trying another candidate.
	DispatchReschedules uint64 `json:"dispatch_reschedules"`
	// DispatchRescheduleExhausted means a request only saw retryable failures, but no
	// remaining schedulable worker could complete it.
	DispatchRescheduleExhausted uint64               `json:"dispatch_reschedule_exhausted"`
	RequestCacheHits            uint64               `json:"request_cache_hits"`
	RequestInProgressConflicts  uint64               `json:"request_in_progress_conflicts"`
	RequestIDConflicts          uint64               `json:"request_id_conflicts"`
	RequestTracker              *RequestTrackerStats `json:"request_tracker,omitempty"`
}

type WorkerMetrics struct {
	ExecutionSuccess     uint64           `json:"execution_success"`
	ExecutionFailure     uint64           `json:"execution_failure"`
	ModuleDigestInvalid  uint64           `json:"module_digest_invalid"`
	ModuleDigestMismatch uint64           `json:"module_digest_mismatch"`
	Cache                ModuleCacheStats `json:"cache"`
}

type ModuleCacheStats struct {
	Entries    int   `json:"entries"`
	Bytes      int64 `json:"bytes"`
	MaxEntries int   `json:"max_entries"`
	MaxBytes   int64 `json:"max_bytes"`
}

type RequestTrackerStats struct {
	Entries    int    `json:"entries"`
	InFlight   int    `json:"in_flight"`
	Completed  int    `json:"completed"`
	MaxEntries int    `json:"max_entries"`
	TTLSeconds int64  `json:"ttl_seconds"`
	Evictions  uint64 `json:"evictions"`
	Expired    uint64 `json:"expired"`
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

func (m *Metrics) IncDispatchReschedule() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatchReschedules++
}

func (m *Metrics) IncDispatchRescheduleExhausted() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatchRescheduleExhausted++
}

func (m *Metrics) IncRequestCacheHit() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestCacheHits++
}

func (m *Metrics) IncRequestInProgressConflict() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestInProgressConflicts++
}

func (m *Metrics) IncRequestIDConflict() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestIDConflicts++
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

func (m *Metrics) IncWorkerModuleDigestInvalid() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.workerModuleDigestInvalid++
}

func (m *Metrics) IncWorkerModuleDigestMismatch() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.workerModuleDigestMismatch++
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
	return m.MasterSnapshotWithRequestTracker(activeWorkers, workersByState, oldestHeartbeatSeconds, nil)
}

func (m *Metrics) MasterSnapshotWithRequestTracker(activeWorkers int, workersByState map[string]int, oldestHeartbeatSeconds *int64, requestTracker *RequestTrackerStats) MetricsResponse {
	response := m.SnapshotBase("master", "")

	m.mu.RLock()
	defer m.mu.RUnlock()
	response.Master = &MasterMetrics{
		ActiveWorkers:               activeWorkers,
		WorkersByState:              workersByState,
		OldestHeartbeatS:            oldestHeartbeatSeconds,
		DispatchSuccess:             m.dispatchSuccess,
		DispatchFailure:             m.dispatchFailure,
		DispatchReschedules:         m.dispatchReschedules,
		DispatchRescheduleExhausted: m.dispatchRescheduleExhausted,
		RequestCacheHits:            m.requestCacheHits,
		RequestInProgressConflicts:  m.requestInProgressConflicts,
		RequestIDConflicts:          m.requestIDConflicts,
		RequestTracker:              requestTracker,
	}

	return response
}

func (m *Metrics) WorkerSnapshot(nodeID string, cache ModuleCacheStats) MetricsResponse {
	response := m.SnapshotBase("worker", nodeID)

	m.mu.RLock()
	defer m.mu.RUnlock()
	response.Worker = &WorkerMetrics{
		ExecutionSuccess:     m.workerExecutionSuccess,
		ExecutionFailure:     m.workerExecutionFailure,
		ModuleDigestInvalid:  m.workerModuleDigestInvalid,
		ModuleDigestMismatch: m.workerModuleDigestMismatch,
		Cache:                cache,
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
