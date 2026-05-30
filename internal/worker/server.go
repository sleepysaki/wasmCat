// all files in the same folder should be in the same package
package worker

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json" // turn go struct into json and vice versa
	"fmt"
	"net/http" // http server to listen for requests from the main process and respond with results
	"os"
	"path/filepath"
	"wasmcat/internal/shared"
)

// Dependency injection -> inject engine into server struct so the server can call its methods
type WorkerServer struct {
	Engine *WasmEngine
	NodeID string
}

func (s *WorkerServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleReady)
	mux.HandleFunc("/invoke", s.handleInvoke)
	return mux
}

func (s *WorkerServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, http.StatusOK, shared.HealthResponse{
		Status: "ok",
		NodeID: s.NodeID,
		Role:   "worker",
	})
}

func (s *WorkerServer) handleReady(w http.ResponseWriter, r *http.Request) {
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

// Logic handler
func (s *WorkerServer) handleInvoke(w http.ResponseWriter, r *http.Request) {
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

	// Now that JSON is valid, ask the engine to run the module.
	// r.Context() connects execution to the HTTP request, so cancellation can flow downward.
	result, err := s.Engine.Execute(r.Context(), req.ModuleName, req.ModuleURL, req.Payload, req.JITBearerToken)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, "execution_failed", err)
		return
	}

	// Wrap the output in the shared response model so the master and user see the same shape.
	resp := shared.ExecutionResponse{
		Result: result,
	}
	shared.WriteJSON(w, http.StatusOK, resp)
}

func (s *WorkerServer) Start(port string) error {
	caPEM, err := os.ReadFile(filepath.Join("./certs", "ca.crt"))
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

	certFile := filepath.Join("./certs", fmt.Sprintf("worker-%s.crt", s.NodeID))
	keyFile := filepath.Join("./certs", fmt.Sprintf("worker-%s.key", s.NodeID))
	return server.ListenAndServeTLS(certFile, keyFile)
}
