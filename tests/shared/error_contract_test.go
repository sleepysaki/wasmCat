package shared_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wasmcat/internal/shared"
)

func TestWriteErrorReturnsSafePublicMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	internalErr := errors.New(`open C:\wasmcat\certs\master.key: access denied`)

	shared.WriteError(rec, http.StatusServiceUnavailable, "dispatch_failed", internalErr)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "dispatch_failed" {
		t.Fatalf("expected dispatch_failed code, got %q", response.Code)
	}
	if response.Error != "Execution could not be dispatched." {
		t.Fatalf("expected safe public message, got %q", response.Error)
	}
	if strings.Contains(response.Error, "master.key") || strings.Contains(response.Error, "access denied") {
		t.Fatalf("public error leaked internal detail: %q", response.Error)
	}
}

func TestPublicErrorMessageFallsBackForUnknownCodes(t *testing.T) {
	if got := shared.PublicErrorMessage("new_unknown_code"); got != "Request failed." {
		t.Fatalf("expected fallback public message, got %q", got)
	}
}

func TestPublicErrorMessageCoversRequestIdempotencyCodes(t *testing.T) {
	tests := map[string]string{
		"request_in_progress": "Execution request is already in progress.",
		"request_id_conflict": "Request ID was already used for different request content.",
	}

	for code, expected := range tests {
		if got := shared.PublicErrorMessage(code); got != expected {
			t.Fatalf("expected %s message %q, got %q", code, expected, got)
		}
	}
}

func TestPublicErrorMessageCoversModuleDigestCodes(t *testing.T) {
	tests := map[string]string{
		"module_digest_invalid":  "Module digest is invalid.",
		"module_digest_mismatch": "Module digest does not match downloaded content.",
	}

	for code, expected := range tests {
		if got := shared.PublicErrorMessage(code); got != expected {
			t.Fatalf("expected %s message %q, got %q", code, expected, got)
		}
	}
}

func TestRequireMethodRejectsUnexpectedMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/execute", nil)
	rec := httptest.NewRecorder()

	ok := shared.RequireMethod(rec, req, http.MethodPost)

	if ok {
		t.Fatal("expected method guard to reject request")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("expected Allow POST, got %q", rec.Header().Get("Allow"))
	}

	var response shared.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "method_not_allowed" {
		t.Fatalf("expected method_not_allowed code, got %q", response.Code)
	}
}
