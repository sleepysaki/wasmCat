package shared

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func WriteError(w http.ResponseWriter, status int, code string, err error) {
	if err != nil {
		slog.Warn("request failed", "status", status, "code", code, "error", err)
	}

	WriteJSON(w, status, ErrorResponse{
		Error: PublicErrorMessage(code),
		Code:  code,
	})
}

func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}

	w.Header().Set("Allow", method)
	WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
	return false
}

func PublicErrorMessage(code string) string {
	switch code {
	case "method_not_allowed":
		return "HTTP method is not allowed for this endpoint."
	case "execute_client_unauthorized":
		return "Execution client is not authorized."
	case "invalid_worker_data":
		return "Invalid worker registration request."
	case "invalid_heartbeat":
		return "Invalid worker heartbeat request."
	case "invalid_drain_request":
		return "Invalid worker drain request."
	case "worker_identity_mismatch":
		return "Worker certificate identity does not match the requested worker ID."
	case "worker_not_found":
		return "Worker is not registered."
	case "invalid_execution_request":
		return "Invalid execution request."
	case "dispatch_failed":
		return "Execution could not be dispatched."
	case "execution_failed":
		return "WASM execution failed."
	case "not_ready":
		return "Service is not ready."
	case "worker_failed":
		return "Worker request failed."
	default:
		return "Request failed."
	}
}
