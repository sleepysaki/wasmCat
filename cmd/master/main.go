package main

import (
	"log"
	"time"
	"wasmcat/internal/config"
	"wasmcat/internal/master"
	"wasmcat/internal/security"
)

func main() {
	log.Println("Initializing Control plane...")

	cfg, err := config.LoadMaster()
	if err != nil {
		log.Fatalf("failed to load master config: %v", err)
	}

	// Dependency Injection & Initialization

	// Create the State Registry
	// Initialize the thread-safe map (Mutex) for tracking workers.
	reg := master.NewRegistry()
	log.Println("State Registry initialized.")

	// Create the Scheduler
	sched := &master.Scheduler{}
	log.Println("Spatial Scheduler initialized.")

	// Create the Dispatcher, need both the Registry and the Scheduler
	dispatch := &master.Dispatcher{
		Registry:  reg,
		Scheduler: sched,
	}
	log.Println("Execution Dispatcher initialized.")

	// Create the API Gateway, need the Registry (to handle /register and /heartbeat) and the Dispatcher (to handle /api/v1/execute)
	gateway := &master.Gateway{
		Registry:   reg,
		Dispatcher: dispatch,
	}

	// Background Processes
	if err := security.GenerateCAAndCerts(cfg.WorkerIDForCert); err != nil {
		log.Fatalf("failed to generate local certs: %v", err)
	}

	// Start the background garbage collection
	// Run completely independently of the web server
	// Every 15 seconds, it scrubs the Registry for dead edge nodes
	go func() {
		log.Printf("Background Reaper started (%s interval).", cfg.CleanupInterval)
		for {
			time.Sleep(cfg.CleanupInterval)
			reg.Cleanup()
		}
	}()

	// Ignition

	// Turn on the API Server
	// This is a blocking call. The program will stay on this line forever unless the server crashes.
	log.Printf("Master Gateway is LIVE on port %s.\n", cfg.Port)

	err = gateway.Start(cfg.Port)
	if err != nil {
		log.Fatalf("CRITICAL: Master Gateway crashed: %v", err)
	}
}
