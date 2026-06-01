package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"wasmcat/internal/bootstrap"
	"wasmcat/internal/config"
	"wasmcat/internal/logging"
	"wasmcat/internal/worker"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	logging.Configure("worker")

	// empty context that can be used to set timeouts, cancel fnc, etc
	// := is a shorthand for declaring and initializing a variable in one line -> create new variable called ctx and assign it the value of context.Background()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		Engine:  engine,
		NodeID:  cfg.NodeID,
		CertDir: cfg.CertDir,
	}

	// This runs in the background and pings the Master every 5 seconds
	slog.Info("starting telemetry pulse", "master_url", cfg.MasterURL, "worker_id", cfg.NodeID, "interval", cfg.HeartbeatInterval.String())
	go worker.StartTelemetry(ctx, cfg.MasterURL, cfg.NodeID, cfg.AdvertiseAddress, cfg.HeartbeatInterval, cfg.CertDir)

	slog.Info("worker server live", "port", cfg.Port, "worker_id", cfg.NodeID)
	err = server.Start(ctx, cfg.Port)
	if err != nil {
		slog.Error("worker server crashed", "error", err)
	}
}

func runInit(args []string) error {
	configDirDefault := defaultConfigDir()

	flags := flag.NewFlagSet("wasmcat-worker init", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	configDir := flags.String("config-dir", configDirDefault, "directory for worker.env")
	certDir := flags.String("cert-dir", "", "directory for mTLS certificates; defaults to <config-dir>/certs")
	workerID := flags.String("worker-id", "worker-vn-01", "stable worker node ID")
	port := flags.String("port", "7271", "worker HTTPS port")
	masterURL := flags.String("master-url", "https://localhost:7270", "master gateway URL")
	advertiseAddress := flags.String("advertise-address", "", "address the master uses to call this worker")
	heartbeatInterval := flags.String("heartbeat-interval", "5s", "master heartbeat interval")
	executionTimeout := flags.String("execution-timeout", "5s", "maximum execution duration")
	moduleFetchTimeout := flags.String("module-fetch-timeout", "10s", "maximum module fetch duration")
	maxModuleBytes := flags.Int64("max-module-bytes", 10<<20, "maximum downloaded module size")
	maxPayloadBytes := flags.Int64("max-payload-bytes", 1<<20, "maximum request payload size")
	maxOutputBytes := flags.Uint64("max-output-bytes", 1<<20, "maximum WASM output size")
	maxConcurrentExecs := flags.Int("max-concurrent-execs", 4, "maximum concurrent executions")
	force := flags.Bool("force", false, "overwrite existing generated files")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if *maxOutputBytes > (1<<32)-1 {
		return fmt.Errorf("max-output-bytes must fit in uint32")
	}

	result, err := bootstrap.InitWorker(bootstrap.WorkerOptions{
		ConfigDir:          *configDir,
		CertDir:            *certDir,
		WorkerID:           *workerID,
		Port:               *port,
		MasterURL:          *masterURL,
		AdvertiseAddress:   *advertiseAddress,
		HeartbeatInterval:  *heartbeatInterval,
		ExecutionTimeout:   *executionTimeout,
		ModuleFetchTimeout: *moduleFetchTimeout,
		MaxModuleBytes:     *maxModuleBytes,
		MaxPayloadBytes:    *maxPayloadBytes,
		MaxOutputBytes:     uint32(*maxOutputBytes),
		MaxConcurrentExecs: *maxConcurrentExecs,
		Force:              *force,
	})
	if err != nil {
		return err
	}

	fmt.Printf("created worker config: %s\n", result.ConfigPath)
	fmt.Printf("using certificate directory: %s\n", result.CertDir)
	for _, warning := range result.Warnings {
		fmt.Printf("next: %s\n", warning)
	}

	return nil
}

func defaultConfigDir() string {
	if runtime.GOOS == "windows" {
		return `C:\wasmcat`
	}

	return "/etc/wasmcat"
}
