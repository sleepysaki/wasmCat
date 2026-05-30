package master

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"wasmcat/internal/logging"
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

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/wasmcat/health", g.handleHealth)
	mux.HandleFunc("/wasmcat/ready", g.handleReady)
	mux.HandleFunc("/internal/register", g.handleRegister)
	mux.HandleFunc("/internal/heartbeat", g.handleHeartbeat)
	mux.HandleFunc("/api/v1/execute", g.handleExecute)
	return logging.Middleware("master", mux)
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{
		Status: "ok",
		Role:   "master",
	})
}

func (g *Gateway) handleReady(w http.ResponseWriter, r *http.Request) {
	// Readiness means the gateway has the dependencies needed to accept and dispatch work.
	// A live process with a nil registry or dispatcher should not receive traffic yet.
	if g.Registry == nil || g.Dispatcher == nil || g.Dispatcher.Scheduler == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, "not_ready", fmt.Errorf("master dependencies are not initialized"))
		return
	}

	shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{
		Status: "ready",
		Role:   "master",
	})
}

func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Create empty box for the incoming worker data, decode the JSON from the request body into that box, and check for errors
	var node shared.WorkerNode
	err := json.NewDecoder(r.Body).Decode(&node)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_worker_data", err)
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
	shared.WriteJSON(w, http.StatusOK, response)
}

func (g *Gateway) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var beat shared.Heartbeat

	err := json.NewDecoder(r.Body).Decode(&beat)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_heartbeat", err)
		return
	}

	// Update the worker status in the registry using the heartbeat payload
	if err := g.Registry.UpdateWorkerStatus(beat); err != nil {
		shared.WriteError(w, http.StatusNotFound, "worker_not_found", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Start the HTTP server and set up the routes for worker registration and heartbeat
func (g *Gateway) Start(ctx context.Context, port string) error {
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
		Handler:   g.Handler(),
		TLSConfig: serverTLSConfig,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServeTLS(filepath.Join("./certs", "master.crt"), filepath.Join("./certs", "master.key"))
	}()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("master gateway shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown master gateway: %w", err)
		}
		if err := <-errCh; err != nil && err != http.ErrServerClosed {
			return err
		}
		slog.Info("master gateway stopped")
		return nil
	}
}

func (g *Gateway) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req shared.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	if err := req.Validate(); err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}

	// Tell the Dispatcher to find a worker and run the code
	result, err := g.Dispatcher.Dispatch(r.Context(), req)
	if err != nil {
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return
	}

	shared.WriteJSON(w, http.StatusOK, result)
}
