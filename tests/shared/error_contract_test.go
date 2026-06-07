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
