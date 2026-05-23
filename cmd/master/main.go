package main

import (
	"log"
	"time"
	"wasmcat/internal/master"
	"wasmcat/internal/security"
)

func main() {
	log.Println("Initializing Control plane...")

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
	if err := security.GenerateCAAndCerts("worker-vn-01"); err != nil {
		log.Fatalf("failed to generate local certs: %v", err)
	}

	// Start the background garbage collection
	// Run completely independently of the web server
	// Every 15 seconds, it scrubs the Registry for dead edge nodes
	go func() {
		log.Println("Background Reaper started (15s interval).")
		for {
			time.Sleep(15 * time.Second)
			reg.Cleanup()
		}
	}()

	// Ignition

	// Turn on the API Server
	// This is a blocking call. The program will stay on this line forever unless the server crashes.
	port := "7270"
	log.Printf("Master Gateway is LIVE on port %s.\n", port)

	err := gateway.Start(port)
	if err != nil {
		log.Fatalf("CRITICAL: Master Gateway crashed: %v", err)
	}
}
