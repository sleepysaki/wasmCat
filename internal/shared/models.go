package shared

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	WorkerStateReady    = "ready"
	WorkerStateDraining = "draining"
	RequestIDMaxLength  = 128
)

// Master node stores these in State Registry
type WorkerNode struct {
	ID        string    `json:"id"`
	IPAddress string    `json:"ip_address"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	CPUFree   float64   `json:"cpu_free"`
	RAMFreeMB float64   `json:"ram_free_mb"`
	LastSeen  time.Time `json:"last_seen"`
	State     string    `json:"state,omitempty"`
}

// Heartbeat: the payload sent every 5 seconds by the Worker to update its status
// IP or GPS coordinates will not be sent again to save network bandwidth
type Heartbeat struct {
	NodeID    string  `json:"node_id"`
	CPUFree   float64 `json:"cpu_free"`
	RAMFreeMB float64 `json:"ram_free_mb"`
}

type DrainRequest struct {
	NodeID string `json:"node_id"`
}

// EXECUTION MODELS (Data Plane)

// ExecutionRequest: payload sent from the End-User to the Master, then forwarded from the Master to the chosen Worker
type ExecutionRequest struct {
	// RequestID is the stable identity for one execution request.
	// Clients may provide it for correlation, or the master creates one before dispatching.
	RequestID string `json:"request_id,omitempty"`

	// The target Wasm module to run
	ModuleName string `json:"module_name"`

	// The raw string or JSON data to process
	Payload           string `json:"payload"`
	ModuleURL         string `json:"module_url"`
	ModuleRegistryURL string `json:"module_registry_url,omitempty"`
	ModuleDigest      string `json:"module_digest,omitempty"`
	JITBearerToken    string `json:"jit_bearer_token,omitempty"`

	// User's location to run Haversine formula
	// "omitempty" means if the Master forwards this to the Worker, it can drop these
	// fields to save bandwidth, since the Worker doesn't care about GPS.
	UserLat float64 `json:"user_lat,omitempty"`
	UserLon float64 `json:"user_lon,omitempty"`
}

func (r ExecutionRequest) Validate() error {
	if err := ValidateRequestID(r.RequestID); err != nil {
		return err
	}
	if r.ModuleName == "" {
		return fmt.Errorf("module_name is required")
	}
	if r.ModuleURL == "" && r.ModuleRegistryURL == "" {
		return fmt.Errorf("module_url or module_registry_url is required")
	}

	return nil
}

func NewRequestID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}

	return "req_" + hex.EncodeToString(bytes[:]), nil
}

func EnsureRequestID(requestID string) (string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return NewRequestID()
	}
	if err := ValidateRequestID(requestID); err != nil {
		return "", err
	}

	return requestID, nil
}

func ValidateRequestID(requestID string) error {
	if requestID == "" {
		return nil
	}
	if len(requestID) > RequestIDMaxLength {
		return fmt.Errorf("request_id must be at most %d characters", RequestIDMaxLength)
	}
	for _, char := range requestID {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '_', '-', '.', ':':
			continue
		default:
			return fmt.Errorf("request_id contains unsupported character %q", char)
		}
	}

	return nil
}

// ExecutionResponse is what the Worker returns to the Master,
// and what the Master returns to the End-User.
type ExecutionResponse struct {
	RequestID string `json:"request_id,omitempty"`

	// The actual output returned from the Wasm linear memory
	Result string `json:"result"`

	// For benchmarks later, measure exec time
	ExecutionTimeMs float64 `json:"execution_time_ms"`

	ExecutedOnNodeID string `json:"executed_on_node_id"`

	// If something breaks inside wazero, it passes the error here
	Error string `json:"error,omitempty"`
}

// UTILITY MODELS

// APIResponse is a standard wrapper for basic success/failure messages
// (e.g., when a worker registers successfully, return this).
type APIResponse struct {
	Status  string `json:"status"`  // "success" or "error"
	Message string `json:"message"` // e.g., "Worker registered successfully"
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
	NodeID string `json:"node_id,omitempty"`
	Role   string `json:"role,omitempty"`
}
