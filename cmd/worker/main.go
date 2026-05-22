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
		log.Fatalf("Failed to load module: ", err)
	}

	server := &worker.WorkerServer{
		Engine: engine,
	}

	log.Println("Worker server is running on port 7271...")

	err = server.Start("7271")
	if err != nil {
		log.Fatalf("Failed to start server: ", err)
	}
}
