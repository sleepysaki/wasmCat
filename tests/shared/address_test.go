package shared_test

import (
	"strings"
	"testing"
	"wasmcat/internal/shared"
)

func TestDefaultWorkerAdvertiseAddressUsesRequestedPort(t *testing.T) {
	address := shared.DefaultWorkerAdvertiseAddress("9444")

	if address == "" {
		t.Fatal("expected a default advertise address")
	}
	if !strings.HasSuffix(address, ":9444") {
		t.Fatalf("expected default address to keep port 9444, got %q", address)
	}
	if strings.HasPrefix(address, ":") {
		t.Fatalf("expected default address to include a host, got %q", address)
	}
}
