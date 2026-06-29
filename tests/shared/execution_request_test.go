package shared_test

import (
	"testing"
	"wasmcat/internal/shared"
)

func TestExecutionRequestValidatesABI(t *testing.T) {
	base := shared.ExecutionRequest{ModuleName: "m", ModuleURL: "https://example.com/m.wasm"}

	for _, abi := range []string{shared.ModuleABIAuto, shared.ModuleABIWasmcat, shared.ModuleABIWASI} {
		req := base
		req.ModuleABI = abi
		if err := req.Validate(); err != nil {
			t.Fatalf("abi %q should be valid, got %v", abi, err)
		}
	}

	req := base
	req.ModuleABI = "not-a-real-abi"
	if err := req.Validate(); err == nil {
		t.Fatal("expected invalid abi to be rejected")
	}
}
