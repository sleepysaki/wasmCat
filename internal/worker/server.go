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

	resp := shared.ExecutionResponse{
		Result: result,
	}
	json.NewEncoder(w).Encode(resp)
}
