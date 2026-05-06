// all files in the same folder should be in the same package
package worker

import (
	"encoding/json" // turn go struct into json and vice versa
	"net/http"      // http server to listen for requests from the main process and respond with results
	"wasmcat/internal/shared"
)

// Dependency injection -> inject engine into server struct so the server can call its methods
type WorkerServer struct {
	Engine *WasmEngine
}

// Logic handler
func (s *WorkerServer) handleInvoke(w http.ResponseWriter, r *http.Request) {
	// create a variable of type ExecutionRequest and decode the JSON body into it
	var req shared.ExecutionRequest
	// tell the decoder to read from the request body and decode into the req struct
	// & means its value is stored at an address, so we can modify it inside the function
	err := json.NewDecoder(r.Body).Decode(&req)
	result, err := s.Engine.Execute(r.Context(), req.ModuleName, req.Payload)

	// Error handling in case user send bad JSON / nonexistent module
	if err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp := shared.ExecutionResponse{
		Result: result,
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *WorkerServer) Start(port string) error {
	// run handleInvoke when getting a request to /invoke endpoint
	http.HandleFunc("/invoke", s.handleInvoke)
	// start the server on the specified port and listen for requests
	return http.ListenAndServe(":"+port, nil)
}
