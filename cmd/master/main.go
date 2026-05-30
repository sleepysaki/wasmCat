package main

import (
	"log/slog"
	"time"
	"wasmcat/internal/config"
	"wasmcat/internal/logging"
	"wasmcat/internal/master"
	"wasmcat/internal/security"
)

func main() {
	logging.Configure("master")
	slog.Info("initializing control plane")

	cfg, err := config.LoadMaster()
	if err != nil {
		slog.Error("failed to load master config", "error", err)
		return
	}

	// Dependency Injection & Initialization

	// Create the State Registry
	// Initialize the thread-safe map (Mutex) for tracking workers.
	reg := master.NewRegistry()
	slog.Info("state registry initialized")

	// Create the Scheduler
	sched := &master.Scheduler{}
	slog.Info("spatial scheduler initialized")

	// Create the Dispatcher, need both the Registry and the Scheduler
	dispatch := &master.Dispatcher{
		Registry:  reg,
		Scheduler: sched,
	}
	slog.Info("execution dispatcher initialized")

	// Create the API Gateway, need the Registry (to handle /register and /heartbeat) and the Dispatcher (to handle /api/v1/execute)
	gateway := &master.Gateway{
		Registry:   reg,
		Dispatcher: dispatch,
	}

	// Background Processes
	if err := security.GenerateCAAndCerts(cfg.WorkerIDForCert); err != nil {
		slog.Error("failed to generate local certs", "error", err)
		return
	}

	// Start the background garbage collection
	// Run completely independently of the web server
	// Every 15 seconds, it scrubs the Registry for dead edge nodes
	go func() {
		slog.Info("background reaper started", "interval", cfg.CleanupInterval.String())
		for {
			time.Sleep(cfg.CleanupInterval)
			reg.Cleanup()
		}
	}()

	// Ignition

	// Turn on the API Server
	// This is a blocking call. The program will stay on this line forever unless the server crashes.
	slog.Info("master gateway live", "port", cfg.Port)

	err = gateway.Start(cfg.Port)
	if err != nil {
		slog.Error("master gateway crashed", "error", err)
	}
}
