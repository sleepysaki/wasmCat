package main

import (
	"context"
	"log"
	"wasmcat/internal/worker"
)

func main() {
	// empty context that can be used to set timeouts, cancel fnc, etc
	// := is a shorthand for declaring and initializing a variable in one line -> create new variable called ctx and assign it the value of context.Background()
	ctx := context.Background()

	engine := worker.NewWasmEngine(ctx)

	err := engine.LoadModule(ctx, "hello", "modules/hello.wasm")

	// if there is an error loading the module, print the error and exit the program
	if err != nil {
		log.Fatalf("Failed to load module: %v", err) // Fixed: Added %v
	}

	server := &worker.WorkerServer{
		Engine: engine,
	}

	// This runs in the background and pings the Master every 5 seconds
	log.Println("Starting telemetry pulse to Master node...")
	go worker.StartTelemetry(ctx, "http://localhost:7270", "worker-vn-01", "localhost:7271")

	log.Println("Worker server is running on port 7271...")
	err = server.Start("7271")
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
