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
		http.Error(w, "Bad Request: invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Now that JSON is valid, ask the engine to run the module.
	// r.Context() connects execution to the HTTP request, so cancellation can flow downward.
	result, err := s.Engine.Execute(r.Context(), req.ModuleName, req.ModuleURL, req.Payload, req.JITBearerToken)
	if err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Wrap the output in the shared response model so the master and user see the same shape.
	resp := shared.ExecutionResponse{
		Result: result,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *WorkerServer) Start(port string) error {
	// run handleInvoke when getting a request to /invoke endpoint
	http.HandleFunc("/invoke", s.handleInvoke)

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
		TLSConfig: tlsConfig,
	}

	certFile := filepath.Join("./certs", fmt.Sprintf("worker-%s.crt", s.NodeID))
	keyFile := filepath.Join("./certs", fmt.Sprintf("worker-%s.key", s.NodeID))
	return server.ListenAndServeTLS(certFile, keyFile)
}
