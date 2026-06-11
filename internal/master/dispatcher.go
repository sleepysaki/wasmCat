package master

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

type Dispatcher struct {
	// Coordinator pattern
	// group scheduler and registry for dispatcher to use
	Registry  *Registry
	Scheduler *Scheduler
	Client    *http.Client
	CertDir   string
}

// workerDispatchError marks whether a worker forwarding failure is safe enough
// for the dispatcher to try another worker in the same request flow.
type workerDispatchError struct {
	workerID  string
	retryable bool
	err       error
}

func (e *workerDispatchError) Error() string {
	return e.err.Error()
}

func (e *workerDispatchError) Unwrap() error {
	return e.err
}

func (d *Dispatcher) Dispatch(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
	start := time.Now()
	moduleRegistryURL := req.ModuleURL
	if moduleRegistryURL == "" {
		moduleRegistryURL = req.ModuleRegistryURL
	}
	req.ModuleURL = moduleRegistryURL
	req.ModuleRegistryURL = moduleRegistryURL

	if isACRURL(moduleRegistryURL) {
		ref, err := parseACRModuleReference(moduleRegistryURL)
		if err != nil {
			return shared.ExecutionResponse{}, err
		}
		token, err := GenerateACRToken(ctx, ref.RegistryName, ref.RepositoryName)
		if err != nil {
			return shared.ExecutionResponse{}, err
		}
		req.JITBearerToken = token

		resolvedModuleURL, moduleDigest, err := resolveACRModuleURL(ctx, moduleRegistryURL, token, ref)
		if err != nil {
			return shared.ExecutionResponse{}, err
		}
		req.ModuleURL = resolvedModuleURL
		req.ModuleDigest = moduleDigest
	}

	workers := d.Registry.GetSchedulableWorkers()
	if len(workers) == 0 {
		return shared.ExecutionResponse{}, fmt.Errorf("no schedulable workers available")
	}

	candidates := workers
	attempt := 0
	var lastErr error

	for len(candidates) > 0 {
		// Re-run scheduling after every retryable failure instead of walking the
		// slice directly, so capacity and location rules still decide the next worker.
		attempt++
		targetNode, err := d.Scheduler.SelectWorker(req.UserLat, req.UserLon, candidates)
		if err != nil {
			return shared.ExecutionResponse{}, err
		}
		slog.Info("worker selected",
			"request_id", req.RequestID,
			"module_name", req.ModuleName,
			"worker_id", targetNode.ID,
			"worker_address", targetNode.IPAddress,
			"active_workers", len(candidates),
			"attempt", attempt,
		)

		resp, err := d.forwardToWorker(ctx, targetNode, req)
		if err == nil {
			slog.Info("dispatch completed",
				"request_id", req.RequestID,
				"module_name", req.ModuleName,
				"worker_id", targetNode.ID,
				"attempt", attempt,
				"duration_ms", time.Since(start).Milliseconds(),
			)

			return resp, nil
		}

		lastErr = err
		if ctx.Err() != nil || !isRetryableDispatchError(err) {
			slog.Error("dispatch failed",
				"request_id", req.RequestID,
				"module_name", req.ModuleName,
				"worker_id", targetNode.ID,
				"attempt", attempt,
				"duration_ms", time.Since(start).Milliseconds(),
				"error", err,
			)
			return shared.ExecutionResponse{}, err
		}

		// Only retry by removing this worker from the local candidate list.
		// The registry itself is left unchanged because heartbeat cleanup owns cluster state.
		candidates = removeWorkerCandidate(candidates, targetNode)
		if len(candidates) == 0 {
			break
		}

		slog.Warn("dispatch attempt failed, trying another worker",
			"request_id", req.RequestID,
			"module_name", req.ModuleName,
			"worker_id", targetNode.ID,
			"attempt", attempt,
			"remaining_workers", len(candidates),
			"error", err,
		)
	}

	slog.Error("dispatch failed on all retryable worker candidates",
		"request_id", req.RequestID,
		"module_name", req.ModuleName,
		"attempts", attempt,
		"duration_ms", time.Since(start).Milliseconds(),
		"error", lastErr,
	)
	return shared.ExecutionResponse{}, lastErr
}

func (d *Dispatcher) forwardToWorker(ctx context.Context, node shared.WorkerNode, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
	// Convert the request to JSON bytes
	data, err := json.Marshal(req)
	if err != nil {
		return shared.ExecutionResponse{}, fmt.Errorf("marshal worker request: %w", err)
	}

	// Build the Worker's URL
	url := workerInvokeURL(node.IPAddress)

	client := d.Client
	if client == nil {
		certDir := d.CertDir
		if certDir == "" {
			certDir = "./certs"
		}
		client, err = security.NewMTLSHTTPClient(security.MasterCertPath(certDir), security.MasterKeyPath(certDir), security.CACertPath(certDir))
		if err != nil {
			return shared.ExecutionResponse{}, err
		}
	}

	// Send the request
	reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		return shared.ExecutionResponse{}, fmt.Errorf("create worker request: %w", err)
	}
	reqHTTP.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(reqHTTP)
	if err != nil {
		return shared.ExecutionResponse{}, newWorkerDispatchError(node.ID, true, err)
	}
	defer resp.Body.Close()
	slog.Info("worker response received", "worker_id", node.ID, "status", resp.StatusCode)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("worker %s returned %s: %s", node.ID, resp.Status, strings.TrimSpace(string(body)))
		return shared.ExecutionResponse{}, newWorkerDispatchError(node.ID, isRetryableWorkerStatus(resp.StatusCode), err)
	}

	// Decode the Worker's result
	var execResp shared.ExecutionResponse
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		return shared.ExecutionResponse{}, fmt.Errorf("decode worker response: %w", err)
	}
	execResp.ExecutedOnNodeID = node.ID
	if execResp.RequestID == "" {
		execResp.RequestID = req.RequestID
	}

	return execResp, nil
}

func newWorkerDispatchError(workerID string, retryable bool, err error) error {
	return &workerDispatchError{
		workerID:  workerID,
		retryable: retryable,
		err:       err,
	}
}

func isRetryableDispatchError(err error) bool {
	var dispatchErr *workerDispatchError
	return errors.As(err, &dispatchErr) && dispatchErr.retryable
}

func isRetryableWorkerStatus(statusCode int) bool {
	// Gateway-style statuses usually mean the selected worker path was temporarily unavailable.
	// Worker execution errors use other statuses and must not be replayed automatically.
	switch statusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func removeWorkerCandidate(workers []shared.WorkerNode, selected shared.WorkerNode) []shared.WorkerNode {
	candidates := make([]shared.WorkerNode, 0, len(workers)-1)
	for _, worker := range workers {
		if worker.ID == selected.ID && worker.IPAddress == selected.IPAddress {
			continue
		}
		candidates = append(candidates, worker)
	}

	return candidates
}

func workerInvokeURL(address string) string {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return strings.TrimRight(address, "/") + "/invoke"
	}

	return fmt.Sprintf("https://%s/invoke", strings.TrimPrefix(address, "https://"))
}
