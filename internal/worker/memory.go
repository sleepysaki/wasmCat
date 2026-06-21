package worker

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// Allocate memory in the Wasm module by calling malloc function
func Allocate(ctx context.Context, mod api.Module, size uint32) (uint32, error) {
	// Find the Wasm "malloc" function.
	// wasmCat's first ABI expects modules to provide malloc so the host can ask
	// the module where it is safe to write input bytes in the module's memory.
	malloc := mod.ExportedFunction("malloc")
	if malloc == nil {
		return 0, fmt.Errorf("module does not export malloc")
	}

	// Call malloc inside the sandbox.
	// The module decides where the allocation lives and returns a pointer as an i32.
	results, err := malloc.Call(ctx, uint64(size))
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("malloc returned no pointer")
	}

	return uint32(results[0]), nil
}

// Write stuff into the Wasm module's memory by first allocating space and then writing bytes
func WriteString(ctx context.Context, mod api.Module, input string) (uint32, error) {
	// Strings are bytes at this boundary.
	// The host writes exactly len(input) bytes and passes that length to run.
	size := uint32(len(input))

	// Get a pointer from Wasm
	ptr, err := Allocate(ctx, mod, size)
	if err != nil {
		return 0, err
	}

	// Reach into the sandbox's RAM and write the bytes
	// mod.Memory() is the module's exported linear memory, not Go's memory.
	if mod.Memory() == nil {
		return 0, fmt.Errorf("module does not export memory")
	}
	success := mod.Memory().Write(ptr, []byte(input))
	if !success {
		return 0, fmt.Errorf("could not write to Wasm memory")
	}

	return ptr, nil
}

// Read a string from the Wasm module's memory by reading bytes and converting back to Go string
func ReadString(mod api.Module, ptr uint32, length uint32) (string, error) {
	// The module returns only a pointer and length.
	// The host still has to bounds-check the read against the module's memory.
	if mod.Memory() == nil {
		return "", fmt.Errorf("module does not export memory")
	}
	data, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return "", fmt.Errorf("out of memory bounds")
	}

	// Copy the memory region into a Go string.
	// Wazero keeps the sandboxed memory separate from the Go heap.
	return string(data), nil
}
