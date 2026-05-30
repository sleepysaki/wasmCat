package main

import (
	"context"
	"log/slog"
	"wasmcat/internal/config"
	"wasmcat/internal/logging"
	"wasmcat/internal/worker"
)

func main() {
	logging.Configure("worker")

	// empty context that can be used to set timeouts, cancel fnc, etc
	// := is a shorthand for declaring and initializing a variable in one line -> create new variable called ctx and assign it the value of context.Background()
	ctx := context.Background()

	cfg, err := config.LoadWorker()
	if err != nil {
		slog.Error("failed to load worker config", "error", err)
		return
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
	slog.Info("starting telemetry pulse", "master_url", cfg.MasterURL, "worker_id", cfg.NodeID, "interval", cfg.HeartbeatInterval.String())
	go worker.StartTelemetry(ctx, cfg.MasterURL, cfg.NodeID, cfg.AdvertiseAddress, cfg.HeartbeatInterval)

	slog.Info("worker server live", "port", cfg.Port, "worker_id", cfg.NodeID)
	err = server.Start(cfg.Port)
	if err != nil {
		slog.Error("worker server crashed", "error", err)
	}
}
