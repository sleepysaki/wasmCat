package master

import (
	"bytes"
	"context"
	"encoding/json"
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

	targetNode, err := d.Scheduler.SelectWorker(req.UserLat, req.UserLon, workers)
	if err != nil {
		return shared.ExecutionResponse{}, err
	}
	slog.Info("worker selected",
		"request_id", req.RequestID,
		"module_name", req.ModuleName,
		"worker_id", targetNode.ID,
		"worker_address", targetNode.IPAddress,
		"active_workers", len(workers),
	)

	resp, err := d.forwardToWorker(ctx, targetNode, req)
	if err != nil {
		slog.Error("dispatch failed",
			"request_id", req.RequestID,
			"module_name", req.ModuleName,
			"worker_id", targetNode.ID,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return shared.ExecutionResponse{}, err
	}

	slog.Info("dispatch completed",
		"request_id", req.RequestID,
		"module_name", req.ModuleName,
		"worker_id", targetNode.ID,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return resp, nil
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
		return shared.ExecutionResponse{}, err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(reqHTTP)
	if err != nil {
		return shared.ExecutionResponse{}, err
	}
	defer resp.Body.Close()
	slog.Info("worker response received", "worker_id", node.ID, "status", resp.StatusCode)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return shared.ExecutionResponse{}, fmt.Errorf("worker %s returned %s: %s", node.ID, resp.Status, strings.TrimSpace(string(body)))
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

func workerInvokeURL(address string) string {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return strings.TrimRight(address, "/") + "/invoke"
	}

	return fmt.Sprintf("https://%s/invoke", strings.TrimPrefix(address, "https://"))
}
