package master

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"wasmcat/internal/shared"
)

type Dispatcher struct {
	// Coordinator pattern
	// group scheduler and registry for dispatcher to use
	Registry  *Registry
	Scheduler *Scheduler
}

func (d *Dispatcher) Dispatch(req shared.ExecutionRequest) (shared.ExecutionResponse, error) {
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
	url := fmt.Sprintf("http://%s/invoke", node.IPAddress)

	// Send the request
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return shared.ExecutionResponse{}, err
	}
	defer resp.Body.Close()

	// Decode the Worker's result
	var execResp shared.ExecutionResponse
	json.NewDecoder(resp.Body).Decode(&execResp)

	return execResp, nil
}
