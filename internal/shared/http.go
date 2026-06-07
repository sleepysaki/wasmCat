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

func PublicErrorMessage(code string) string {
	switch code {
	case "invalid_worker_data":
		return "Invalid worker registration request."
	case "invalid_heartbeat":
		return "Invalid worker heartbeat request."
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
