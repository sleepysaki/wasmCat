package shared

import (
	"encoding/json"
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
	message := ""
	if err != nil {
		message = err.Error()
	}

	WriteJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}
