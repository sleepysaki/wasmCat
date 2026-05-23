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

	server := &worker.WorkerServer{
		Engine: engine,
		NodeID: "worker-vn-01",
	}

	// This runs in the background and pings the Master every 5 seconds
	log.Println("Starting telemetry pulse to Master node...")
	go worker.StartTelemetry(ctx, "https://localhost:7270", "worker-vn-01", "localhost:7271")

	log.Println("Worker server is running on port 7271...")
	err := server.Start("7271")
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
