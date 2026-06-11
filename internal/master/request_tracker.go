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

const (
	DefaultExecutionRequestCacheTTL        = 5 * time.Minute
	DefaultExecutionRequestCacheMaxEntries = 4096
)

type ExecutionRequestDecision int

const (
	ExecutionRequestStarted ExecutionRequestDecision = iota
	ExecutionRequestInFlight
	ExecutionRequestCompleted
	ExecutionRequestConflict
)

type ExecutionRequestTracker struct {
	mu         sync.Mutex
	entries    map[string]trackedExecutionRequest
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

type trackedExecutionRequest struct {
	fingerprint string
	state       ExecutionRequestDecision
	response    shared.ExecutionResponse
	completedAt time.Time
	lastSeenAt  time.Time
}

type ExecutionRequestStatus struct {
	Decision ExecutionRequestDecision
	Response shared.ExecutionResponse
}

func NewExecutionRequestTracker(ttl time.Duration) *ExecutionRequestTracker {
	return NewExecutionRequestTrackerWithLimit(ttl, DefaultExecutionRequestCacheMaxEntries)
}

func NewExecutionRequestTrackerWithLimit(ttl time.Duration, maxEntries int) *ExecutionRequestTracker {
	if ttl <= 0 {
		ttl = DefaultExecutionRequestCacheTTL
	}
	if maxEntries <= 0 {
		maxEntries = DefaultExecutionRequestCacheMaxEntries
	}

	return &ExecutionRequestTracker{
		entries:    make(map[string]trackedExecutionRequest),
		ttl:        ttl,
		maxEntries: maxEntries,
		now:        time.Now,
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

	now := t.now()
	t.removeExpiredCompletedLocked(now)

	entry, exists := t.entries[req.RequestID]
	if !exists {
		t.entries[req.RequestID] = trackedExecutionRequest{
			fingerprint: fingerprint,
			state:       ExecutionRequestStarted,
			lastSeenAt:  now,
		}
		t.evictCompletedOverLimitLocked()
		return ExecutionRequestStatus{Decision: ExecutionRequestStarted}, nil
	}
	if entry.fingerprint != fingerprint {
		return ExecutionRequestStatus{Decision: ExecutionRequestConflict}, nil
	}

	switch entry.state {
	case ExecutionRequestStarted:
		return ExecutionRequestStatus{Decision: ExecutionRequestInFlight}, nil
	case ExecutionRequestCompleted:
		entry.lastSeenAt = now
		t.entries[req.RequestID] = entry
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
	now := t.now()
	entry.completedAt = now
	entry.lastSeenAt = now
	t.entries[requestID] = entry
	t.evictCompletedOverLimitLocked()
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

func (t *ExecutionRequestTracker) evictCompletedOverLimitLocked() {
	for len(t.entries) > t.maxEntries {
		var oldestRequestID string
		var oldestSeen time.Time
		foundCompleted := false
		for requestID, entry := range t.entries {
			if entry.state != ExecutionRequestCompleted {
				continue
			}
			if !foundCompleted || entry.lastSeenAt.Before(oldestSeen) {
				oldestRequestID = requestID
				oldestSeen = entry.lastSeenAt
				foundCompleted = true
			}
		}
		if !foundCompleted {
			return
		}
		delete(t.entries, oldestRequestID)
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
