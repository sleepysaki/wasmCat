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
func StartTelemetry(ctx context.Context, masterURL string, nodeID string, workerAddress string) {
	registerWorker(masterURL, nodeID, workerAddress)
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
			registerWorker(masterURL, nodeID, workerAddress)
			// Send heartbeat to Master
			sendHeartbeat(masterURL, nodeID)
		}
	}
}

func registerWorker(masterURL string, nodeID string, workerAddress string) {
	node := shared.WorkerNode{
		ID:        nodeID,
		IPAddress: workerAddress,
	}

	data, err := json.Marshal(node)
	if err != nil {
		log.Printf("Telemetry register error: %v\n", err)
		return
	}

	endpoint := masterURL + "/internal/register"
	resp, err := http.Post(endpoint, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("Failed to register worker with Master: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		log.Printf("Worker registration failed: %s", resp.Status)
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
	if resp.StatusCode >= http.StatusBadRequest {
		log.Printf("Heartbeat rejected: %s", resp.Status)
	}
	// Close the response body to prevent network leaks
	defer resp.Body.Close()
}
