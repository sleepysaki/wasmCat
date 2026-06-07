// all files in the same folder should be in the same package
package worker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json" // turn go struct into json and vice versa
	"fmt"
	"log/slog"
	"net/http" // http server to listen for requests from the main process and respond with results
	"os"
	"time"
	"wasmcat/internal/logging"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

// Dependency injection -> inject engine into server struct so the server can call its methods
type WorkerServer struct {
	Engine  *WasmEngine
	NodeID  string
	CertDir string
	Metrics *shared.Metrics
}

func (s *WorkerServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/wasmcat/health", s.handleHealth)
	mux.HandleFunc("/wasmcat/ready", s.handleReady)
	mux.HandleFunc("/wasmcat/metrics", s.handleMetrics)
	mux.HandleFunc("/invoke", s.handleInvoke)
	return logging.MiddlewareWithMetrics("worker", mux, s.metrics())
}

func (s *WorkerServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodGet) {
		return
	}

	shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{
		Status: "ok",
		NodeID: s.NodeID,
		Role:   "worker",
	})
}

func (s *WorkerServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodGet) {
		return
	}

	// Readiness means the worker has the engine dependency needed to execute modules.
	// This does not prove a specific module URL is reachable; that remains per-request work.
	if s.Engine == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, "not_ready", fmt.Errorf("worker engine is not initialized"))
		return
	}

	shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{
		Status: "ready",
		NodeID: s.NodeID,
		Role:   "worker",
	})
}

func (s *WorkerServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodGet) {
		return
	}

	var cacheStats shared.ModuleCacheStats
	if s.Engine != nil {
		cacheStats = s.Engine.CacheStats()
	}

	shared.WriteJSON(w, http.StatusOK, s.metrics().WorkerSnapshot(s.NodeID, cacheStats))
}

// Logic handler
func (s *WorkerServer) handleInvoke(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodPost) {
		return
	}
	if s.Engine == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, "not_ready", fmt.Errorf("worker engine is not initialized"))
		return
	}

	start := time.Now()

	// Limit the HTTP body before JSON decoding.
	// The payload limit covers the user data, and the extra bytes leave room for JSON field names and module metadata.
	r.Body = http.MaxBytesReader(w, r.Body, s.Engine.Limits().MaxPayloadBytes+4096)

	// create a variable of type ExecutionRequest and decode the JSON body into it
	var req shared.ExecutionRequest
	// tell the decoder to read from the request body and decode into the req struct
	// & means its value is stored at an address, so we can modify it inside the function
	err := json.NewDecoder(r.Body).Decode(&req)
	// Error handling in case user send bad JSON / nonexistent module
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	if err := req.Validate(); err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	requestID, err := shared.EnsureRequestID(req.RequestID)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	req.RequestID = requestID

	// Now that JSON is valid, ask the engine to run the module.
	// r.Context() connects execution to the HTTP request, so cancellation can flow downward.
	slog.Info("worker execution request received", "request_id", req.RequestID, "module_name", req.ModuleName, "module_digest", req.ModuleDigest)
	result, err := s.Engine.ExecuteWithDigest(r.Context(), req.ModuleName, req.ModuleURL, req.ModuleDigest, req.Payload, req.JITBearerToken)
	if err != nil {
		s.metrics().IncWorkerExecutionFailure()
		slog.Error("worker execution request failed", "request_id", req.RequestID, "module_name", req.ModuleName, "duration_ms", time.Since(start).Milliseconds(), "error", err)
		shared.WriteError(w, http.StatusBadRequest, "execution_failed", err)
		return
	}
	s.metrics().IncWorkerExecutionSuccess()
	durationMs := float64(time.Since(start).Microseconds()) / 1000

	// Wrap the output in the shared response model so the master and user see the same shape.
	resp := shared.ExecutionResponse{
		RequestID:        req.RequestID,
		Result:           result,
		ExecutionTimeMs:  durationMs,
		ExecutedOnNodeID: s.NodeID,
	}
	slog.Info("worker execution request completed", "request_id", req.RequestID, "module_name", req.ModuleName, "duration_ms", durationMs)
	shared.WriteJSON(w, http.StatusOK, resp)
}

func (s *WorkerServer) metrics() *shared.Metrics {
	if s.Metrics == nil {
		s.Metrics = shared.NewMetrics()
	}

	return s.Metrics
}

func (s *WorkerServer) Start(ctx context.Context, port string) error {
	certDir := s.CertDir
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

	tlsConfig := &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12,
	}

	server := &http.Server{
		Addr:      ":" + port,
		Handler:   s.Handler(),
		TLSConfig: tlsConfig,
	}

	certFile := security.WorkerCertPath(certDir, s.NodeID)
	keyFile := security.WorkerKeyPath(certDir, s.NodeID)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServeTLS(certFile, keyFile)
	}()

	select {
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("worker server shutdown requested", "worker_id", s.NodeID)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown worker server: %w", err)
		}
		if err := <-errCh; err != nil && err != http.ErrServerClosed {
			return err
		}
		slog.Info("worker server stopped", "worker_id", s.NodeID)
		return nil
	}
}
