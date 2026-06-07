package shared_test

import (
	"strings"
	"testing"
	"wasmcat/internal/shared"
)

func TestNewRequestIDReturnsValidGeneratedID(t *testing.T) {
	t.Parallel()

	requestID, err := shared.NewRequestID()
	if err != nil {
		t.Fatalf("new request id failed: %v", err)
	}
	if !strings.HasPrefix(requestID, "req_") {
		t.Fatalf("expected req_ prefix, got %q", requestID)
	}
	if err := shared.ValidateRequestID(requestID); err != nil {
		t.Fatalf("generated request id should be valid: %v", err)
	}
}

func TestEnsureRequestIDPreservesValidClientID(t *testing.T) {
	t.Parallel()

	requestID, err := shared.EnsureRequestID("client:deploy-42")
	if err != nil {
		t.Fatalf("ensure request id failed: %v", err)
	}
	if requestID != "client:deploy-42" {
		t.Fatalf("expected client id to be preserved, got %q", requestID)
	}
}

func TestValidateRequestIDRejectsUnsupportedCharacters(t *testing.T) {
	t.Parallel()

	err := shared.ValidateRequestID("bad id with spaces")
	if err == nil {
		t.Fatal("expected unsupported character error")
	}
}

func TestValidateRequestIDRejectsTooLongID(t *testing.T) {
	t.Parallel()

	err := shared.ValidateRequestID(strings.Repeat("a", shared.RequestIDMaxLength+1))
	if err == nil {
		t.Fatal("expected max length error")
	}
}
