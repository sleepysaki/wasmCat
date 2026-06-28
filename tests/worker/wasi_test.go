package worker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"wasmcat/internal/shared"
	"wasmcat/internal/worker"
)

// TestWasmEngineExecutesWASIModule proves the worker can run a standard wasip1
// production module (compiled here with the Go toolchain) that reads its input
// from stdin and writes its result to stdout, alongside the custom ABI.
func TestWasmEngineExecutesWASIModule(t *testing.T) {
	if testing.Short() {
		t.Skip("wasi module build is skipped in short mode")
	}

	module := buildWASIEchoModule(t)
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(module)
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		MaxModuleBytes:     64 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 2,
	})

	// Explicit WASI mode.
	result, err := engine.ExecuteWithDigestAndABI(ctx, "wasi-echo", moduleServer.URL, "", "hello", "", shared.ModuleABIWASI)
	if err != nil {
		t.Fatalf("explicit wasi execute failed: %v", err)
	}
	if result != "wasi:hello" {
		t.Fatalf("expected %q, got %q", "wasi:hello", result)
	}

	// Auto-detection: the module exports _start, so an empty ABI must resolve to
	// WASI without the caller specifying it.
	result, err = engine.ExecuteWithDigestAndABI(ctx, "wasi-echo", moduleServer.URL, "", "world", "", shared.ModuleABIAuto)
	if err != nil {
		t.Fatalf("auto-detected wasi execute failed: %v", err)
	}
	if result != "wasi:world" {
		t.Fatalf("expected %q, got %q", "wasi:world", result)
	}
}

// TestWasmEngineAutoDetectsCustomABI confirms the original custom-ABI module
// still runs under auto-detection (it exports run, not _start).
func TestWasmEngineAutoDetectsCustomABI(t *testing.T) {
	ctx := context.Background()
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	result, err := engine.ExecuteWithDigestAndABI(ctx, "echo", moduleServer.URL, "", "abi-auto", "", shared.ModuleABIAuto)
	if err != nil {
		t.Fatalf("auto-detected custom ABI execute failed: %v", err)
	}
	if result != "abi-auto" {
		t.Fatalf("expected echoed payload, got %q", result)
	}
}

// buildWASIEchoModule compiles a minimal wasip1 command module that prefixes its
// stdin with "wasi:" and writes it to stdout.
func buildWASIEchoModule(t *testing.T) []byte {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	source := `package main

import (
	"io"
	"os"
)

func main() {
	os.Stdout.WriteString("wasi:")
	io.Copy(os.Stdout, os.Stdin)
}
`
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatalf("write wasi source: %v", err)
	}

	out := filepath.Join(dir, "mod.wasm")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build wasip1 module with this toolchain: %v\n%s", err, string(output))
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read built wasi module: %v", err)
	}

	return data
}
