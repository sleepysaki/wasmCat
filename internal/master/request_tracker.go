package master

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"wasmcat/internal/shared"
)

const DefaultExecutionRequestCacheTTL = 5 * time.Minute

type ExecutionRequestDecision int

const (
	ExecutionRequestStarted ExecutionRequestDecision = iota
	ExecutionRequestInFlight
	ExecutionRequestCompleted
	ExecutionRequestConflict
)

type ExecutionRequestTracker struct {
	mu      sync.Mutex
	entries map[string]trackedExecutionRequest
	ttl     time.Duration
	now     func() time.Time
}

type trackedExecutionRequest struct {
	fingerprint string
	state       ExecutionRequestDecision
	response    shared.ExecutionResponse
	completedAt time.Time
}

type ExecutionRequestStatus struct {
	Decision ExecutionRequestDecision
	Response shared.ExecutionResponse
}

func NewExecutionRequestTracker(ttl time.Duration) *ExecutionRequestTracker {
	if ttl <= 0 {
		ttl = DefaultExecutionRequestCacheTTL
	}

	return &ExecutionRequestTracker{
		entries: make(map[string]trackedExecutionRequest),
		ttl:     ttl,
		now:     time.Now,
	}
}

func (t *ExecutionRequestTracker) Begin(req shared.ExecutionRequest) (ExecutionRequestStatus, error) {
	if t == nil || req.RequestID == "" {
		return ExecutionRequestStatus{Decision: ExecutionRequestStarted}, nil
	}

	fingerprint, err := executionRequestFingerprint(req)
	if err != nil {
		return ExecutionRequestStatus{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.removeExpiredCompletedLocked(t.now())

	entry, exists := t.entries[req.RequestID]
	if !exists {
		t.entries[req.RequestID] = trackedExecutionRequest{
			fingerprint: fingerprint,
			state:       ExecutionRequestStarted,
		}
		return ExecutionRequestStatus{Decision: ExecutionRequestStarted}, nil
	}
	if entry.fingerprint != fingerprint {
		return ExecutionRequestStatus{Decision: ExecutionRequestConflict}, nil
	}

	switch entry.state {
	case ExecutionRequestStarted:
		return ExecutionRequestStatus{Decision: ExecutionRequestInFlight}, nil
	case ExecutionRequestCompleted:
		return ExecutionRequestStatus{
			Decision: ExecutionRequestCompleted,
			Response: entry.response,
		}, nil
	default:
		return ExecutionRequestStatus{Decision: ExecutionRequestInFlight}, nil
	}
}

func (t *ExecutionRequestTracker) Complete(requestID string, response shared.ExecutionResponse) {
	if t == nil || requestID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, exists := t.entries[requestID]
	if !exists {
		return
	}
	entry.state = ExecutionRequestCompleted
	entry.response = response
	entry.completedAt = t.now()
	t.entries[requestID] = entry
}

func (t *ExecutionRequestTracker) Forget(requestID string) {
	if t == nil || requestID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, requestID)
}

func (t *ExecutionRequestTracker) removeExpiredCompletedLocked(now time.Time) {
	for requestID, entry := range t.entries {
		if entry.state == ExecutionRequestCompleted && now.Sub(entry.completedAt) > t.ttl {
			delete(t.entries, requestID)
		}
	}
}

func executionRequestFingerprint(req shared.ExecutionRequest) (string, error) {
	// JITBearerToken is intentionally excluded because the master may mint it per dispatch.
	// The client-visible execution identity is the module reference, digest, payload, and user location.
	identity := struct {
		ModuleName        string  `json:"module_name"`
		Payload           string  `json:"payload"`
		ModuleURL         string  `json:"module_url"`
		ModuleRegistryURL string  `json:"module_registry_url"`
		ModuleDigest      string  `json:"module_digest"`
		UserLat           float64 `json:"user_lat"`
		UserLon           float64 `json:"user_lon"`
	}{
		ModuleName:        req.ModuleName,
		Payload:           req.Payload,
		ModuleURL:         req.ModuleURL,
		ModuleRegistryURL: req.ModuleRegistryURL,
		ModuleDigest:      req.ModuleDigest,
		UserLat:           req.UserLat,
		UserLon:           req.UserLon,
	}

	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("fingerprint execution request: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
