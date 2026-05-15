package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
	"wasmcat/internal/shared"
)

// StartTelemetry begins sending heartbeats to the Master node.
// masterURL should be something like "http://localhost:8080"
func StartTelemetry(ctx context.Context, masterURL string, nodeID string) {
	// Create a ticker that fires every 5 seconds
	// time.Sleep() in a loop is not used because it can block thread, no easy cancellation, and less accurate
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit the function
			log.Println("Telemetry stopped")
			return
		case <-ticker.C:
			// Send heartbeat to Master
			sendHeartbeat(masterURL, nodeID)
		}
	}
}

// Construct a Heartbeat struct, convert to JSON, send to the Master node via HTTP POST
func sendHeartbeat(masterURL string, nodeID string) {
	// Create the payload, temp hardcode value
	beat := shared.Heartbeat{
		NodeID:    nodeID,
		CPUFree:   95.5,
		RAMFreeMB: 2048,
	}

	// Convert struct to JSON bytes
	data, err := json.Marshal(beat)
	if err != nil {
		log.Printf("Telemetry error: %v\n", err)
		return
	}

	// Send the HTTP POST request to the Master
	endpoint := masterURL + "/internal/heartbeat"
	resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(data))

	if err != nil {
		log.Printf("Failed to reach Master: %v\n", err)
		return
	}
	// Close the response body to prevent network leaks
	defer resp.Body.Close()
}
