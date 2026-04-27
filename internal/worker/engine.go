package worker

import (
	"context" // manage lifecycle, kill fnc after timeout to save resources
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
)

type WasmEngine struct {
	// Store wazero.Runtime aka engine and image caches to avoid reloading and recompiling for each execution
	runtime	wazero.Runtime
	cache	map[string]wazero.CompiledModule
	mu		sync.RWMutex
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