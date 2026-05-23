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
	e.mu.RLock()
	_, ok := e.cache[moduleName]
	e.mu.RUnlock()
	if ok {
		return nil
	}

	if moduleURL == "" {
		return fmt.Errorf("module %s has no module URL", moduleName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, moduleURL, nil)
	if err != nil {
		return fmt.Errorf("create request for %s: %w", moduleURL, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", moduleURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", moduleURL, resp.Status)
	}

	wasmBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", moduleURL, err)
	}

	compiled, err := e.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compile %s: %w", moduleName, err)
	}

	e.mu.Lock()
	if _, exists := e.cache[moduleName]; !exists {
		e.cache[moduleName] = compiled
	}
	e.mu.Unlock()

	return nil
}

func (e *WasmEngine) Execute(ctx context.Context, moduleName string, moduleURL string, payload string) (string, error) {
	if err := e.FetchAndCache(ctx, moduleName, moduleURL); err != nil {
		return "", err
	}

	e.mu.RLock()
	_, ok := e.cache[moduleName]
	e.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("module %s not loaded", moduleName)
	}

	// Placeholder execution: echo the payload for now
	return payload, nil
}
