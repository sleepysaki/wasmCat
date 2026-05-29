package master

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

type Dispatcher struct {
	// Coordinator pattern
	// group scheduler and registry for dispatcher to use
	Registry  *Registry
	Scheduler *Scheduler
}

func (d *Dispatcher) Dispatch(ctx context.Context, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
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

		resolvedModuleURL, err := resolveACRModuleURL(ctx, moduleRegistryURL, token, ref)
		if err != nil {
			return shared.ExecutionResponse{}, err
		}
		req.ModuleURL = resolvedModuleURL
	}

	workers := d.Registry.GetActiveWorkers()
	if len(workers) == 0 {
		return shared.ExecutionResponse{}, fmt.Errorf("no active workers available")
	}

	targetNode, err := d.Scheduler.SelectWorker(req.UserLat, req.UserLon, workers)
	if err != nil {
		return shared.ExecutionResponse{}, err
	}

	return d.forwardToWorker(targetNode, req)
}

func (d *Dispatcher) forwardToWorker(node shared.WorkerNode, req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
	// Convert the request to JSON bytes
	data, _ := json.Marshal(req)

	// Build the Worker's URL
	url := fmt.Sprintf("https://%s/invoke", strings.TrimPrefix(node.IPAddress, "https://"))

	client, err := security.NewMTLSHTTPClient(filepath.Join("./certs", "master.crt"), filepath.Join("./certs", "master.key"), filepath.Join("./certs", "ca.crt"))
	if err != nil {
		return shared.ExecutionResponse{}, err
	}

	// Send the request
	reqHTTP, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		return shared.ExecutionResponse{}, err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(reqHTTP)
	if err != nil {
		return shared.ExecutionResponse{}, err
	}
	defer resp.Body.Close()

	// Decode the Worker's result
	var execResp shared.ExecutionResponse
	json.NewDecoder(resp.Body).Decode(&execResp)

	return execResp, nil
}
