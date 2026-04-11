package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/tetratelabs/wazero"
)

func main() {
	// 1. Create a background context (wazero requires this to track execution)
	ctx := context.Background()

	// 2. Read the WASM file
	wasmBytes, err := os.ReadFile("math.wasm")
	if err != nil {
		log.Fatalf("Failed to read WASM file: %v", err)
	}

	// 3. Create a new WASM runtime (using 'wazero', not 'wasm')
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx) // Always close the runtime when done

	// 4. Instantiate the module into the runtime
	mod, err := r.Instantiate(ctx, wasmBytes)
	if err != nil {
		log.Fatalf("Failed to instantiate WASM module: %v", err)
	}

	// 5. Find the exported function
	addFunc := mod.ExportedFunction("add")
	if addFunc == nil {
		log.Fatalf("Could not find 'add' function in math.wasm")
	}

	// 6. Call the addition function (passing the numbers 5 and 3)
	// Wasm functions take and return 64-bit integers by default
	result, err := addFunc.Call(ctx, 5, 3)
	if err != nil {
		log.Fatalf("Failed to call add function: %v", err)
	}

	// 7. Print the result! result[0] holds the first returned value
	fmt.Printf("5 + 3 = %d\n", result[0])
}