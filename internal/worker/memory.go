package worker

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// Allocate memory in the Wasm module by calling malloc function
func Allocate(ctx context.Context, mod api.Module, size uint32) (uint32, error) {
	// Find the Wasm "malloc" function
	malloc := mod.ExportedFunction("malloc")

	results, err := malloc.Call(ctx, uint64(size))
	if err != nil {
		return 0, err
	}

	return uint32(results[0]), nil
}

// Write stuff into the Wasm module's memory by first allocating space and then writing bytes
func WriteString(ctx context.Context, mod api.Module, input string) (uint32, error) {
	size := uint32(len(input))

	// 1. Get a pointer from Wasm
	ptr, err := Allocate(ctx, mod, size)
	if err != nil {
		return 0, err
	}

	// 2. Reach into the sandbox's RAM and write the bytes
	success := mod.Memory().Write(ptr, []byte(input))
	if !success {
		return 0, fmt.Errorf("could not write to Wasm memory")
	}

	return ptr, nil
}

// Read a string from the Wasm module's memory by reading bytes and converting back to Go string
func ReadString(mod api.Module, ptr uint32, length uint32) (string, error) {
	data, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return "", fmt.Errorf("out of memory bounds")
	}

	return string(data), nil
}
