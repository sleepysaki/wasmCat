package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

// StartTelemetry begins sending heartbeats to the Master node.
// masterURL should be something like "https://localhost:7270"
func StartTelemetry(ctx context.Context, masterURL string, nodeID string, workerAddress string, latitude float64, longitude float64, interval time.Duration, certDir string) {
	StartTelemetryWithMetrics(ctx, masterURL, nodeID, workerAddress, latitude, longitude, interval, certDir, SystemMetricsProvider{})
}

func StartTelemetryWithMetrics(ctx context.Context, masterURL string, nodeID string, workerAddress string, latitude float64, longitude float64, interval time.Duration, certDir string, metricsProvider MetricsProvider) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if certDir == "" {
		certDir = "./certs"
	}
	if metricsProvider == nil {
		metricsProvider = SystemMetricsProvider{}
	}

	client, err := newMTLSClient(certDir, nodeID)
	if err != nil {
		slog.Error("telemetry client init error", "worker_id", nodeID, "error", err)
		return
	}

	registerWorker(client, masterURL, nodeID, workerAddress, latitude, longitude)
	sendHeartbeat(ctx, client, masterURL, nodeID, metricsProvider)
	// Create a ticker that fires on the configured heartbeat interval
	// time.Sleep() in a loop is not used because it can block thread, no easy cancellation, and less accurate
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit the function
			slog.Info("telemetry stopped", "worker_id", nodeID)
			return
		case <-ticker.C:
			registerWorker(client, masterURL, nodeID, workerAddress, latitude, longitude)
			// Send heartbeat to Master
			sendHeartbeat(ctx, client, masterURL, nodeID, metricsProvider)
		}
	}
}

func newMTLSClient(certDir string, nodeID string) (*http.Client, error) {
	return security.NewMTLSHTTPClient(security.WorkerCertPath(certDir, nodeID), security.WorkerKeyPath(certDir, nodeID), security.CACertPath(certDir))
}

func registerWorker(client *http.Client, masterURL string, nodeID string, workerAddress string, latitude float64, longitude float64) {
	node := shared.WorkerNode{
		ID:        nodeID,
		IPAddress: workerAddress,
		Latitude:  latitude,
		Longitude: longitude,
	}

	data, err := json.Marshal(node)
	if err != nil {
		slog.Error("telemetry register marshal error", "worker_id", nodeID, "error", err)
		return
	}

	endpoint := masterURL + "/internal/register"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(data))
	if err != nil {
		slog.Error("failed to build worker registration request", "worker_id", nodeID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("failed to register worker with master", "worker_id", nodeID, "master_url", masterURL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Warn("worker registration rejected", "worker_id", nodeID, "status", resp.Status)
		return
	}
	slog.Info("worker registration sent", "worker_id", nodeID, "master_url", masterURL, "status", resp.StatusCode)
}

// Construct a Heartbeat struct, convert to JSON, send to the Master node via HTTP POST
func sendHeartbeat(ctx context.Context, client *http.Client, masterURL string, nodeID string, metricsProvider MetricsProvider) {
	metrics, err := metricsProvider.Snapshot(ctx)
	if err != nil {
		slog.Error("heartbeat metrics error", "worker_id", nodeID, "error", err)
		return
	}

	beat := shared.Heartbeat{
		NodeID:    nodeID,
		CPUFree:   metrics.CPUFree,
		RAMFreeMB: metrics.RAMFreeMB,
	}

	// Convert struct to JSON bytes
	data, err := json.Marshal(beat)
	if err != nil {
		slog.Error("heartbeat marshal error", "worker_id", nodeID, "error", err)
		return
	}

	// Send the HTTP POST request to the Master
	endpoint := masterURL + "/internal/heartbeat"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(data))
	if err != nil {
		slog.Error("failed to build heartbeat request", "worker_id", nodeID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		slog.Error("failed to reach master heartbeat endpoint", "worker_id", nodeID, "master_url", masterURL, "error", err)
		return
	}
	if resp.StatusCode >= http.StatusBadRequest {
		slog.Warn("heartbeat rejected", "worker_id", nodeID, "status", resp.Status)
	}
	// Close the response body to prevent network leaks
	defer resp.Body.Close()
}
