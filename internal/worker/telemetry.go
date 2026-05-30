package worker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"wasmcat/internal/shared"
)

// StartTelemetry begins sending heartbeats to the Master node.
// masterURL should be something like "http://localhost:8080"
func StartTelemetry(ctx context.Context, masterURL string, nodeID string, workerAddress string, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	client, err := newMTLSClient(nodeID)
	if err != nil {
		log.Printf("telemetry client init error: %v\n", err)
		return
	}

	registerWorker(client, masterURL, nodeID, workerAddress)
	// Create a ticker that fires on the configured heartbeat interval
	// time.Sleep() in a loop is not used because it can block thread, no easy cancellation, and less accurate
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit the function
			log.Println("Telemetry stopped")
			return
		case <-ticker.C:
			registerWorker(client, masterURL, nodeID, workerAddress)
			// Send heartbeat to Master
			sendHeartbeat(client, masterURL, nodeID)
		}
	}
}

func newMTLSClient(nodeID string) (*http.Client, error) {
	certFile := filepath.Join("./certs", fmt.Sprintf("worker-%s.crt", nodeID))
	keyFile := filepath.Join("./certs", fmt.Sprintf("worker-%s.key", nodeID))
	caFile := filepath.Join("./certs", "ca.crt")

	clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load worker cert: %w", err)
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("append ca cert")
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      caPool,
			MinVersion:   tls.VersionTLS12,
		},
	}

	return &http.Client{Transport: transport}, nil
}

func registerWorker(client *http.Client, masterURL string, nodeID string, workerAddress string) {
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
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(data))
	if err != nil {
		log.Printf("failed to build worker registration request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
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
func sendHeartbeat(client *http.Client, masterURL string, nodeID string) {
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
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(data))
	if err != nil {
		log.Printf("Failed to build heartbeat request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)

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
