package master

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
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

const (
	defaultMaxExecuteBodyBytes   = 2 << 20
	internalControlMaxBodyBytes  = 4 << 10
	DefaultMasterShutdownTimeout = 5 * time.Second
)

// Web server for the Master node
// Put pointer to registry in the gateway struct so that the handlers can access it
type Gateway struct {
	Registry   *Registry
	Dispatcher *Dispatcher
	CertDir    string
	Metrics    *shared.Metrics
	Requests   *ExecutionRequestTracker
	JobStore   JobStore

	// ExecuteClientIDs is optional. When empty, any trusted mTLS client can call
	// /api/v1/execute. When set, the caller certificate must match one of these
	// identities by common name or DNS SAN.
	ExecuteClientIDs []string

	MaxExecuteBodyBytes    int64
	ModulePolicy           ModulePolicy
	RequestCacheTTL        time.Duration
	RequestCacheMaxEntries int
	JobMaxAttempts         int
	JobLeaseTTL            time.Duration
	// How long the master waits for active HTTP requests after SIGINT/SIGTERM.
	// This protects shutdown from hanging forever while still giving in-flight requests a chance to finish.
	ShutdownTimeout time.Duration
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
	mux.HandleFunc("/internal/drain", g.handleDrain)
	mux.HandleFunc("/api/v1/execute", g.handleExecute)
	return logging.MiddlewareWithMetrics("master", mux, g.metrics())
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodGet) {
		return
	}

	shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{
		Status: "ok",
		Role:   "master",
	})
}

func (g *Gateway) handleReady(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodGet) {
		return
	}

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
	if !shared.RequireMethod(w, r, http.MethodGet) {
		return
	}
	g.attachDispatcherMetrics()

	activeWorkers := 0
	workersByState := map[string]int{}
	var oldestHeartbeatSeconds *int64
	if g.Registry != nil {
		activeWorkers = g.Registry.ActiveWorkerCount()
		workersByState = g.Registry.WorkerStateCounts()
		oldestHeartbeatSeconds = g.Registry.OldestHeartbeatAge(time.Now())
	}
	requestTrackerStats := g.requests().Stats()

	shared.WriteJSON(w, http.StatusOK, g.metrics().MasterSnapshotWithRequestTracker(activeWorkers, workersByState, oldestHeartbeatSeconds, &requestTrackerStats))
}

func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodPost) {
		return
	}
	limitRequestBody(w, r, internalControlMaxBodyBytes)

	// Create empty box for the incoming worker data, decode the JSON from the request body into that box, and check for errors
	var node shared.WorkerNode
	err := json.NewDecoder(r.Body).Decode(&node)
	if err != nil {
		if isBodyTooLargeError(err) {
			shared.WriteError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", err)
			return
		}
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
	if !shared.RequireMethod(w, r, http.MethodPost) {
		return
	}
	limitRequestBody(w, r, internalControlMaxBodyBytes)

	var beat shared.Heartbeat

	err := json.NewDecoder(r.Body).Decode(&beat)
	if err != nil {
		if isBodyTooLargeError(err) {
			shared.WriteError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", err)
			return
		}
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

func (g *Gateway) handleDrain(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodPost) {
		return
	}
	limitRequestBody(w, r, internalControlMaxBodyBytes)

	var req shared.DrainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if isBodyTooLargeError(err) {
			shared.WriteError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", err)
			return
		}
		shared.WriteError(w, http.StatusBadRequest, "invalid_drain_request", err)
		return
	}
	if err := validateWorkerPeerIdentity(r, req.NodeID); err != nil {
		shared.WriteError(w, http.StatusForbidden, "worker_identity_mismatch", err)
		return
	}

	if err := g.Registry.DrainWorker(req.NodeID); err != nil {
		shared.WriteError(w, http.StatusNotFound, "worker_not_found", err)
		return
	}

	shared.WriteJSON(w, http.StatusOK, shared.APIResponse{
		Status:  "success",
		Message: "Worker marked as draining",
	})
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

	server := shared.NewHTTPServer(":"+port, g.Handler(), serverTLSConfig)

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
		shutdownTimeout := g.shutdownTimeout()
		slog.Info("master gateway shutdown requested", "shutdown_timeout", shutdownTimeout.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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

func (g *Gateway) shutdownTimeout() time.Duration {
	if g.ShutdownTimeout <= 0 {
		return DefaultMasterShutdownTimeout
	}

	return g.ShutdownTimeout
}

func (g *Gateway) handleExecute(w http.ResponseWriter, r *http.Request) {
	if !shared.RequireMethod(w, r, http.MethodPost) {
		return
	}
	if err := g.validateExecuteClientIdentity(r); err != nil {
		shared.WriteError(w, http.StatusForbidden, "execute_client_unauthorized", err)
		return
	}
	limitRequestBody(w, r, g.maxExecuteBodyBytes())

	var req shared.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if isBodyTooLargeError(err) {
			shared.WriteError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", err)
			return
		}
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	if err := req.Validate(); err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	if err := g.ModulePolicy.Validate(req); err != nil {
		shared.WriteError(w, http.StatusForbidden, "module_policy_violation", err)
		return
	}
	requestID, err := shared.EnsureRequestID(req.RequestID)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	req.RequestID = requestID

	if g.JobStore != nil {
		g.handleDurableExecute(w, r, req)
		return
	}

	requestStatus, err := g.requests().Begin(req)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return
	}
	switch requestStatus.Decision {
	case ExecutionRequestCompleted:
		g.metrics().IncRequestCacheHit()
		shared.WriteJSON(w, http.StatusOK, requestStatus.Response)
		return
	case ExecutionRequestInFlight:
		g.metrics().IncRequestInProgressConflict()
		shared.WriteError(w, http.StatusConflict, "request_in_progress", fmt.Errorf("request_id %q is already running", req.RequestID))
		return
	case ExecutionRequestConflict:
		g.metrics().IncRequestIDConflict()
		shared.WriteError(w, http.StatusConflict, "request_id_conflict", fmt.Errorf("request_id %q was already used for a different execution request", req.RequestID))
		return
	}

	// Tell the Dispatcher to find a worker and run the code
	g.attachDispatcherMetrics()
	result, err := g.Dispatcher.Dispatch(r.Context(), req)
	if err != nil {
		g.requests().Forget(req.RequestID)
		g.metrics().IncDispatchFailure()
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return
	}
	g.requests().Complete(req.RequestID, result)
	g.metrics().IncDispatchSuccess()

	shared.WriteJSON(w, http.StatusOK, result)
}

func (g *Gateway) handleDurableExecute(w http.ResponseWriter, r *http.Request, req shared.ExecutionRequest) {
	ctx := r.Context()

	job, started, ok := g.beginDurableJob(ctx, w, req)
	if !ok {
		return
	}
	if !started {
		// beginDurableJob already wrote the duplicate/conflict response.
		_ = job
		return
	}

	leaseUntil := time.Now().Add(g.jobLeaseTTL())
	if err := g.JobStore.MarkDispatching(ctx, req.RequestID, "", leaseUntil); err != nil {
		g.metrics().IncDispatchFailure()
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return
	}

	g.attachDispatcherMetrics()
	result, err := g.Dispatcher.Dispatch(ctx, req)
	if err != nil {
		_ = g.JobStore.MarkFailed(context.Background(), req.RequestID, err.Error())
		g.metrics().IncDispatchFailure()
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return
	}
	if err := g.JobStore.MarkSucceeded(context.Background(), req.RequestID, result); err != nil {
		g.metrics().IncDispatchFailure()
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return
	}
	g.metrics().IncDispatchSuccess()

	shared.WriteJSON(w, http.StatusOK, result)
}

func (g *Gateway) beginDurableJob(ctx context.Context, w http.ResponseWriter, req shared.ExecutionRequest) (JobRecord, bool, bool) {
	fingerprint, err := ExecutionJobFingerprint(req)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "invalid_execution_request", err)
		return JobRecord{}, false, false
	}

	now := time.Now()
	job := NewJobRecord(req, fingerprint, g.jobMaxAttempts(), now)
	if err := g.JobStore.Create(ctx, job); err == nil {
		return job, true, true
	} else if !errors.Is(err, ErrJobExists) {
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return JobRecord{}, false, false
	}

	existing, err := g.JobStore.Get(ctx, req.RequestID)
	if err != nil {
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", err)
		return JobRecord{}, false, false
	}
	if existing.Fingerprint != fingerprint {
		g.metrics().IncRequestIDConflict()
		shared.WriteError(w, http.StatusConflict, "request_id_conflict", fmt.Errorf("request_id %q was already used for a different execution request", req.RequestID))
		return existing, false, true
	}

	switch existing.Status {
	case JobSucceeded:
		if existing.Response == nil {
			shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", fmt.Errorf("job %q succeeded without a stored response", req.RequestID))
			return existing, false, false
		}
		g.metrics().IncRequestCacheHit()
		shared.WriteJSON(w, http.StatusOK, *existing.Response)
		return existing, false, true
	case JobQueued, JobDispatching, JobRunning:
		g.metrics().IncRequestInProgressConflict()
		shared.WriteError(w, http.StatusConflict, "request_in_progress", fmt.Errorf("request_id %q is already running", req.RequestID))
		return existing, false, true
	case JobFailed:
		if existing.MaxAttempts > 0 && existing.Attempt >= existing.MaxAttempts {
			g.metrics().IncDispatchFailure()
			shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", fmt.Errorf("request_id %q reached max attempts", req.RequestID))
			return existing, false, true
		}
		return existing, true, true
	case JobAmbiguous:
		g.metrics().IncRequestInProgressConflict()
		shared.WriteError(w, http.StatusConflict, "request_in_progress", fmt.Errorf("request_id %q is ambiguous and requires recovery", req.RequestID))
		return existing, false, true
	default:
		shared.WriteError(w, http.StatusServiceUnavailable, "dispatch_failed", fmt.Errorf("job %q has unknown status %q", req.RequestID, existing.Status))
		return existing, false, false
	}
}

func (g *Gateway) metrics() *shared.Metrics {
	if g.Metrics == nil {
		g.Metrics = shared.NewMetrics()
	}

	return g.Metrics
}

func (g *Gateway) requests() *ExecutionRequestTracker {
	if g.Requests == nil {
		g.Requests = NewExecutionRequestTrackerWithLimit(g.requestCacheTTL(), g.requestCacheMaxEntries())
	}

	return g.Requests
}

func (g *Gateway) attachDispatcherMetrics() {
	if g.Dispatcher != nil && g.Dispatcher.Metrics == nil {
		g.Dispatcher.Metrics = g.metrics()
	}
}

func (g *Gateway) maxExecuteBodyBytes() int64 {
	if g.MaxExecuteBodyBytes > 0 {
		return g.MaxExecuteBodyBytes
	}

	return defaultMaxExecuteBodyBytes
}

func (g *Gateway) requestCacheTTL() time.Duration {
	if g.RequestCacheTTL > 0 {
		return g.RequestCacheTTL
	}

	return DefaultExecutionRequestCacheTTL
}

func (g *Gateway) requestCacheMaxEntries() int {
	if g.RequestCacheMaxEntries > 0 {
		return g.RequestCacheMaxEntries
	}

	return DefaultExecutionRequestCacheMaxEntries
}

func (g *Gateway) jobMaxAttempts() int {
	if g.JobMaxAttempts > 0 {
		return g.JobMaxAttempts
	}

	return DefaultJobMaxAttempts
}

func (g *Gateway) jobLeaseTTL() time.Duration {
	if g.JobLeaseTTL > 0 {
		return g.JobLeaseTTL
	}

	return DefaultJobLeaseTTL
}

func limitRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
}

func isBodyTooLargeError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
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

func (g *Gateway) validateExecuteClientIdentity(r *http.Request) error {
	if len(g.ExecuteClientIDs) == 0 {
		return nil
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return fmt.Errorf("execution client certificate is required")
	}

	cert := r.TLS.PeerCertificates[0]
	for _, allowedID := range g.ExecuteClientIDs {
		allowedID = strings.TrimSpace(allowedID)
		if allowedID == "" {
			continue
		}
		if cert.Subject.CommonName == allowedID {
			return nil
		}
		for _, dnsName := range cert.DNSNames {
			if dnsName == allowedID {
				return nil
			}
		}
	}

	return fmt.Errorf("execution client certificate identity %q is not allowed", cert.Subject.CommonName)
}
