// all files in the same folder should be in the same package
package worker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json" // turn go struct into json and vice versa
	"errors"
	"fmt"
	"log/slog"
	"net/http" // http server to listen for requests from the main process and respond with results
	"os"
	"strings"
	"time"
	"wasmcat/internal/logging"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

const DefaultJobCompletionTimeout = 5 * time.Second

// Dependency injection -> inject engine into server struct so the server can call its methods
type WorkerServer struct {
	Engine            *WasmEngine
	NodeID            string
	CertDir           string
	MasterURL         string
	CompletionClient  *http.Client
	CompletionTimeout time.Duration
	Metrics           *shared.Metrics
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

	// The execution context intentionally starts from Background instead of the
	// inbound HTTP request. If the master restarts after dispatching, the worker
	// should still finish bounded execution and report completion through the
	// callback path instead of losing the result with the broken connection.
	slog.Info("worker execution request received", "request_id", req.RequestID, "module_name", req.ModuleName, "module_digest", req.ModuleDigest, "abi", req.ModuleABI)
	result, err := s.Engine.ExecuteWithDigestAndABI(context.Background(), req.ModuleName, req.ModuleURL, req.ModuleDigest, req.Payload, req.JITBearerToken, req.ModuleABI)
	if err != nil {
		errorCode := workerExecutionErrorCode(err)
		s.metrics().IncWorkerExecutionFailure()
		s.observeWorkerExecutionError(errorCode)
		slog.Error("worker execution request failed", "request_id", req.RequestID, "module_name", req.ModuleName, "duration_ms", time.Since(start).Milliseconds(), "error", err)
		s.reportJobCompletion(shared.JobCompletionRequest{
			RequestID: req.RequestID,
			WorkerID:  s.NodeID,
			Status:    shared.JobCompletionFailed,
			Error:     err.Error(),
		})
		shared.WriteError(w, http.StatusBadRequest, errorCode, err)
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
	s.reportJobCompletion(shared.JobCompletionRequest{
		RequestID: req.RequestID,
		WorkerID:  s.NodeID,
		Status:    shared.JobCompletionSucceeded,
		Response:  &resp,
	})
	shared.WriteJSON(w, http.StatusOK, resp)
}

func (s *WorkerServer) reportJobCompletion(completion shared.JobCompletionRequest) {
	masterURL := strings.TrimRight(strings.TrimSpace(s.MasterURL), "/")
	if masterURL == "" {
		return
	}

	payload, err := json.Marshal(completion)
	if err != nil {
		slog.Warn("job completion marshal failed", "request_id", completion.RequestID, "worker_id", completion.WorkerID, "error", err)
		return
	}

	timeout := s.completionTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, masterURL+"/internal/jobs/complete", bytes.NewReader(payload))
	if err != nil {
		slog.Warn("job completion request creation failed", "request_id", completion.RequestID, "worker_id", completion.WorkerID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client, err := s.completionHTTPClient()
	if err != nil {
		slog.Warn("job completion client creation failed", "request_id", completion.RequestID, "worker_id", completion.WorkerID, "error", err)
		return
	}

	resp, err := shared.DoWithRetry(client, req)
	if err != nil {
		slog.Warn("job completion callback failed", "request_id", completion.RequestID, "worker_id", completion.WorkerID, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		slog.Warn("job completion callback rejected", "request_id", completion.RequestID, "worker_id", completion.WorkerID, "status", resp.Status)
		return
	}
	slog.Info("job completion callback recorded", "request_id", completion.RequestID, "worker_id", completion.WorkerID, "status", completion.Status)
}

func (s *WorkerServer) completionHTTPClient() (*http.Client, error) {
	if s.CompletionClient != nil {
		return s.CompletionClient, nil
	}

	certDir := s.CertDir
	if certDir == "" {
		certDir = "./certs"
	}

	return security.NewMTLSHTTPClient(security.WorkerCertPath(certDir, s.NodeID), security.WorkerKeyPath(certDir, s.NodeID), security.CACertPath(certDir))
}

func (s *WorkerServer) completionTimeout() time.Duration {
	if s.CompletionTimeout > 0 {
		return s.CompletionTimeout
	}

	return DefaultJobCompletionTimeout
}

func workerExecutionErrorCode(err error) string {
	var digestErr *ModuleDigestError
	if errors.As(err, &digestErr) {
		if digestErr.Reason == ModuleDigestErrorMismatch {
			return "module_digest_mismatch"
		}
		return "module_digest_invalid"
	}

	return "execution_failed"
}

func (s *WorkerServer) observeWorkerExecutionError(code string) {
	// Keep digest counters separate from the generic failure count.
	// A malformed digest is usually a caller problem; a mismatch can mean remote content drift.
	switch code {
	case "module_digest_invalid":
		s.metrics().IncWorkerModuleDigestInvalid()
	case "module_digest_mismatch":
		s.metrics().IncWorkerModuleDigestMismatch()
	}
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

	server := shared.NewHTTPServer(":"+port, s.Handler(), tlsConfig)

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
		shutdownTimeout := s.shutdownTimeout()
		slog.Info("worker server shutdown requested", "worker_id", s.NodeID, "shutdown_timeout", shutdownTimeout.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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

func (s *WorkerServer) shutdownTimeout() time.Duration {
	if s.Engine == nil {
		return DefaultLimits.ShutdownTimeout
	}

	return s.Engine.Limits().ShutdownTimeout
}
