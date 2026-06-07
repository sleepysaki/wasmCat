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
	"strings"
	"time"
	"wasmcat/internal/logging"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

// Web server for the Master node
// Put pointer to registry in the gateway struct so that the handlers can access it
type Gateway struct {
	Registry   *Registry
	Dispatcher *Dispatcher
	CertDir    string
	Metrics    *shared.Metrics
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
	mux.HandleFunc("/wasmcat/metrics", g.handleMetrics)
	mux.HandleFunc("/internal/register", g.handleRegister)
	mux.HandleFunc("/internal/heartbeat", g.handleHeartbeat)
	mux.HandleFunc("/api/v1/execute", g.handleExecute)
	return logging.MiddlewareWithMetrics("master", mux, g.metrics())
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

func (g *Gateway) handleMetrics(w http.ResponseWriter, r *http.Request) {
	activeWorkers := 0
	var oldestHeartbeatSeconds *int64
	if g.Registry != nil {
		activeWorkers = g.Registry.ActiveWorkerCount()
		oldestHeartbeatSeconds = g.Registry.OldestHeartbeatAge(time.Now())
	}

	shared.WriteJSON(w, http.StatusOK, g.metrics().MasterSnapshot(activeWorkers, oldestHeartbeatSeconds))
}

func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Create empty box for the incoming worker data, decode the JSON from the request body into that box, and check for errors
	var node shared.WorkerNode
	err := json.NewDecoder(r.Body).Decode(&node)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_worker_data", err)
		return
	}
	if err := validateWorkerPeerIdentity(r, node.ID); err != nil {
		shared.WriteError(w, http.StatusForbidden, "worker_identity_mismatch", err)
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
	if err := validateWorkerPeerIdentity(r, beat.NodeID); err != nil {
		shared.WriteError(w, http.StatusForbidden, "worker_identity_mismatch", err)
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
	certDir := g.CertDir
	if certDir == "" {
		certDir = "./certs"
	}

	caPEM, err := os.ReadFile(security.CACertPath(certDir))
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
		errCh <- server.ListenAndServeTLS(security.MasterCertPath(certDir), security.MasterKeyPath(certDir))
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
		g.metrics().IncDispatchFailure()
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return
	}
	g.metrics().IncDispatchSuccess()

	shared.WriteJSON(w, http.StatusOK, result)
}

func (g *Gateway) metrics() *shared.Metrics {
	if g.Metrics == nil {
		g.Metrics = shared.NewMetrics()
	}

	return g.Metrics
}

func validateWorkerPeerIdentity(r *http.Request, workerID string) error {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return fmt.Errorf("worker id is required")
	}

	// Handler-only tests use httptest without TLS. The real Gateway.Start path always
	// requires a verified client certificate before the request reaches this handler.
	if r.TLS == nil {
		return nil
	}
	if len(r.TLS.PeerCertificates) == 0 {
		return fmt.Errorf("worker client certificate is required")
	}

	cert := r.TLS.PeerCertificates[0]
	expectedName := workerCertificateName(workerID)
	if cert.Subject.CommonName == expectedName {
		return nil
	}
	for _, dnsName := range cert.DNSNames {
		if dnsName == workerID || dnsName == expectedName {
			return nil
		}
	}

	return fmt.Errorf("worker certificate identity %q is not allowed to claim worker id %q", cert.Subject.CommonName, workerID)
}

func workerCertificateName(workerID string) string {
	return "wasmcat-worker-" + workerID
}
