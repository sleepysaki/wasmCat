package worker

import (
	"context" // manage lifecycle, kill fnc after timeout to save resources
	"fmt"
	"os"
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

func (e *WasmEngine) LoadModule(ctx context.Context, name string, path string) error {
	// Read file from storage
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Compile
	compiled, err := e.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return err
	}

	// Save in cache
	e.mu.Lock()
	e.cache[name] = compiled
	e.mu.Unlock()

	return nil
}

func (e *WasmEngine) Execute(ctx context.Context, moduleName string, payload string) (string, error) {
	e.mu.RLock()
	_, ok := e.cache[moduleName]
	e.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("module %s not loaded", moduleName)
	}

	// Placeholder execution: echo the payload for now
	return payload, nil
}
