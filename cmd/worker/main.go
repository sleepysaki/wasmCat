package main

import (
	"context"
	"log"
	"wasmcat/internal/config"
	"wasmcat/internal/worker"
)

func main() {
	// empty context that can be used to set timeouts, cancel fnc, etc
	// := is a shorthand for declaring and initializing a variable in one line -> create new variable called ctx and assign it the value of context.Background()
	ctx := context.Background()

	cfg, err := config.LoadWorker()
	if err != nil {
		log.Fatalf("failed to load worker config: %v", err)
	}

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		ExecutionTimeout:   cfg.Limits.ExecutionTimeout,
		ModuleFetchTimeout: cfg.Limits.ModuleFetchTimeout,
		MaxModuleBytes:     cfg.Limits.MaxModuleBytes,
		MaxPayloadBytes:    cfg.Limits.MaxPayloadBytes,
		MaxOutputBytes:     cfg.Limits.MaxOutputBytes,
		MaxConcurrentExecs: cfg.Limits.MaxConcurrentExecs,
	})

	server := &worker.WorkerServer{
		Engine: engine,
		NodeID: cfg.NodeID,
	}

	// This runs in the background and pings the Master every 5 seconds
	log.Println("Starting telemetry pulse to Master node...")
	go worker.StartTelemetry(ctx, cfg.MasterURL, cfg.NodeID, cfg.AdvertiseAddress, cfg.HeartbeatInterval)

	log.Printf("Worker server is running on port %s...", cfg.Port)
	err = server.Start(cfg.Port)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
