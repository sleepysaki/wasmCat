package master

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"wasmcat/internal/shared"
)

// Web server for the Master node
// Put pointer to registry in the gateway struct so that the handlers can access it
type Gateway struct {
	Registry   *Registry
	Dispatcher *Dispatcher
}

// Constructor
func NewGateway(reg *Registry) *Gateway {
	return &Gateway{
		Registry: reg,
	}
}

func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Create empty box for the incoming worker data, decode the JSON from the request body into that box, and check for errors
	var node shared.WorkerNode
	err := json.NewDecoder(r.Body).Decode(&node)
	if err != nil {
		http.Error(w, "Invalid worker data", http.StatusBadRequest)
		return
	}
	// Register the worker in the registry
	g.Registry.RegisterWorker(node)

	// Send a success response back to the worker
	response := shared.APIResponse{
		Status:  "success",
		Message: "Worker registered successfully",
	}
	// Set the response header to indicate that it is sending JSON, encode the response struct as JSON in the response body
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (g *Gateway) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var beat shared.Heartbeat

	err := json.NewDecoder(r.Body).Decode(&beat)
	if err != nil {
		http.Error(w, "Invalid heartbeat data", http.StatusBadRequest)
		return
	}

	// Update the worker status in the registry using the heartbeat payload
	if err := g.Registry.UpdateWorkerStatus(beat); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Start the HTTP server and set up the routes for worker registration and heartbeat
func (g *Gateway) Start(port string) error {
	// Internal routes for the cluster infrastructure
	http.HandleFunc("/internal/register", g.handleRegister)
	http.HandleFunc("/internal/heartbeat", g.handleHeartbeat)
	http.HandleFunc("/api/v1/execute", g.handleExecute)

	caPEM, err := os.ReadFile(filepath.Join("./certs", "ca.crt"))
	if err != nil {
		return fmt.Errorf("read ca cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("append ca cert")
	}

	serverTLSConfig := &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12,
	}

	server := &http.Server{
		Addr:      ":" + port,
		Handler:   nil,
		TLSConfig: serverTLSConfig,
	}

	return server.ListenAndServeTLS(filepath.Join("./certs", "master.crt"), filepath.Join("./certs", "master.key"))
}

func (g *Gateway) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req shared.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid execution request", http.StatusBadRequest)
		return
	}

	// Tell the Dispatcher to find a worker and run the code
	result, err := g.Dispatcher.Dispatch(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	json.NewEncoder(w).Encode(result)
}
