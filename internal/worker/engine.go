package worker

import (
	"context" // manage lifecycle, kill fnc after timeout to save resources
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/tetratelabs/wazero"
)

type WasmEngine struct {
	// Store wazero.Runtime aka engine and image caches to avoid reloading and recompiling for each execution
	runtime wazero.Runtime
	cache   map[string]wazero.CompiledModule
	mu      sync.RWMutex
}

func NewWasmEngine(ctx context.Context) *WasmEngine {
	return &WasmEngine{
		runtime: wazero.NewRuntime(ctx),
		cache:   make(map[string]wazero.CompiledModule),
	}
}

func (e *WasmEngine) FetchAndCache(ctx context.Context, moduleName, moduleURL string) error {
	// First check the cache with a read lock.
	// Read locks let many goroutines check the map at the same time,
	// which is useful because most executions should use an already compiled module.
	e.mu.RLock()
	_, ok := e.cache[moduleName]
	e.mu.RUnlock()
	if ok {
		return nil
	}

	// The worker needs a URL when the module is not in cache yet.
	// Later, the master can point this at ACR, blob storage, or any module store.
	if moduleURL == "" {
		return fmt.Errorf("module %s has no module URL", moduleName)
	}

	// Tie the download request to the execution context.
	// If the caller cancels the request or adds a timeout later, the download stops too.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, moduleURL, nil)
	if err != nil {
		return fmt.Errorf("create request for %s: %w", moduleURL, err)
	}

	// Download the raw .wasm bytes.
	// This is still a simple client; production should add size limits and client timeouts.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", moduleURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", moduleURL, resp.Status)
	}

	// Read the module into memory so wazero can compile it.
	// A production worker should cap this reader so one huge module cannot exhaust RAM.
	wasmBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", moduleURL, err)
	}

	// Compile once and cache the compiled form.
	// Compilation is more expensive than instantiation, so the cache avoids doing it for every request.
	compiled, err := e.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compile %s: %w", moduleName, err)
	}

	// Take the write lock only when changing the map.
	// Another goroutine may have compiled the same module while this one was downloading,
	// so keep the first cached version and drop the duplicate.
	e.mu.Lock()
	if _, exists := e.cache[moduleName]; !exists {
		e.cache[moduleName] = compiled
	}
	e.mu.Unlock()

	return nil
}

func (e *WasmEngine) Execute(ctx context.Context, moduleName string, moduleURL string, payload string) (string, error) {
	// Make sure the module is compiled and available.
	// FetchAndCache downloads only on the first request for this module name.
	if err := e.FetchAndCache(ctx, moduleName, moduleURL); err != nil {
		return "", err
	}

	// Pull the compiled module out of the cache.
	// A compiled module is like a reusable template. It is not the running instance yet.
	e.mu.RLock()
	compiled, ok := e.cache[moduleName]
	e.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("module %s not loaded", moduleName)
	}

	// Instantiate a fresh module for this execution.
	// Each request gets its own instance and its own linear memory, so requests do not share data.
	mod, err := e.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		return "", fmt.Errorf("instantiate module %s: %w", moduleName, err)
	}
	defer mod.Close(ctx)

	// Step 4: copy the input string into the module's linear memory.
	// Go memory and Wasm memory are separate, so we cannot pass a Go string directly.
	inputPtr, err := WriteString(ctx, mod, payload)
	if err != nil {
		return "", fmt.Errorf("write input for %s: %w", moduleName, err)
	}

	// Find the exported entrypoint.
	// Expect modules to export:
	//   run(ptr uint32, len uint32) uint64
	// The uint64 packs the output pointer in the high 32 bits and output length in the low 32 bits.
	run := mod.ExportedFunction("run")
	if run == nil {
		return "", fmt.Errorf("module %s does not export run", moduleName)
	}

	// Call the Wasm function.
	// Wazero uses uint64 values for all Wasm parameters/results at the Go API boundary.
	results, err := run.Call(ctx, uint64(inputPtr), uint64(len(payload)))
	if err != nil {
		return "", fmt.Errorf("run module %s: %w", moduleName, err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("module %s returned no result", moduleName)
	}

	// Unpack the pointer and length returned by the module.
	// This keeps the first ABI small because one Wasm result can carry both values.
	packedOutput := results[0]
	outputPtr := uint32(packedOutput >> 32)
	outputLen := uint32(packedOutput)

	// Read the output bytes back out of Wasm memory and return them as a Go string.
	output, err := ReadString(mod, outputPtr, outputLen)
	if err != nil {
		return "", fmt.Errorf("read output for %s: %w", moduleName, err)
	}

	return output, nil
}
