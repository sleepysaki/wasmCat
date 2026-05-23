package shared

import "time"

// Master node stores these in State Registry
type WorkerNode struct {
	ID        string    `json:"id"`
	IPAddress string    `json:"ip_address"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	CPUFree   float64   `json:"cpu_free"`
	RAMFreeMB float64   `json:"ram_free_mb"`
	LastSeen  time.Time `json:"last_seen"`
}

// Heartbeat: the payload sent every 5 seconds by the Worker to update its status
// IP or GPS coordinates will not be sent again to save network bandwidth
type Heartbeat struct {
	NodeID    string  `json:"node_id"`
	CPUFree   float64 `json:"cpu_free"`
	RAMFreeMB float64 `json:"ram_free_mb"`
}

// EXECUTION MODELS (Data Plane)

// ExecutionRequest: payload sent from the End-User to the Master, then forwarded from the Master to the chosen Worker
type ExecutionRequest struct {
	// The target Wasm module to run
	ModuleName string `json:"module_name"`

	// The raw string or JSON data to process
	Payload   string `json:"payload"`
	ModuleURL string `json:"module_url"`
	ModuleRegistryURL string `json:"module_registry_url,omitempty"`
	JITBearerToken    string `json:"jit_bearer_token,omitempty"`

	// User's location to run Haversine formula
	// "omitempty" means if the Master forwards this to the Worker, it can drop these
	// fields to save bandwidth, since the Worker doesn't care about GPS.
	UserLat float64 `json:"user_lat,omitempty"`
	UserLon float64 `json:"user_lon,omitempty"`
}

// ExecutionResponse is what the Worker returns to the Master,
// and what the Master returns to the End-User.
type ExecutionResponse struct {
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
