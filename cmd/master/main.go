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
	"time"
	"wasmcat/internal/bootstrap"
	"wasmcat/internal/config"
	"wasmcat/internal/logging"
	"wasmcat/internal/master"
	"wasmcat/internal/security"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	logging.Configure("master")
	slog.Info("initializing control plane")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	sched := &master.Scheduler{
		MinCPUFree:   cfg.MinWorkerCPUFree,
		MinRAMFreeMB: cfg.MinWorkerRAMFreeMB,
	}
	slog.Info("capacity-aware scheduler initialized", "min_cpu_free", cfg.MinWorkerCPUFree, "min_ram_free_mb", cfg.MinWorkerRAMFreeMB)

	// Create the Dispatcher, need both the Registry and the Scheduler
	dispatch := &master.Dispatcher{
		Registry:  reg,
		Scheduler: sched,
		CertDir:   cfg.CertDir,
	}
	slog.Info("execution dispatcher initialized")

	// Create the API Gateway, need the Registry (to handle /register and /heartbeat) and the Dispatcher (to handle /api/v1/execute)
	gateway := &master.Gateway{
		Registry:            reg,
		Dispatcher:          dispatch,
		CertDir:             cfg.CertDir,
		ExecuteClientIDs:    cfg.ExecuteClientIDs,
		MaxExecuteBodyBytes: cfg.MaxExecuteBodyBytes,
	}

	// Background Processes
	if cfg.AutoGenerateCerts {
		if err := security.GenerateCAAndCerts(cfg.CertDir, cfg.WorkerIDForCert); err != nil {
			slog.Error("failed to generate local certs", "cert_dir", cfg.CertDir, "error", err)
			return
		}
		slog.Info("local certificates generated", "cert_dir", cfg.CertDir, "worker_id", cfg.WorkerIDForCert)
	} else {
		slog.Info("automatic certificate generation disabled", "cert_dir", cfg.CertDir)
	}

	// Start the background garbage collection
	// Run completely independently of the web server
	// Every 15 seconds, it scrubs the Registry for dead edge nodes
	go func() {
		slog.Info("background reaper started", "interval", cfg.CleanupInterval.String())
		ticker := time.NewTicker(cfg.CleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("background reaper stopped")
				return
			case <-ticker.C:
				reg.Cleanup()
			}
		}
	}()

	// Ignition

	// Turn on the API Server
	// This is a blocking call. The program will stay on this line forever unless the server crashes.
	slog.Info("master gateway live", "port", cfg.Port)

	err = gateway.Start(ctx, cfg.Port)
	if err != nil {
		slog.Error("master gateway crashed", "error", err)
	}
}

func runInit(args []string) error {
	configDirDefault := defaultConfigDir()

	flags := flag.NewFlagSet("wasmcat-master init", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	configDir := flags.String("config-dir", configDirDefault, "directory for master.env")
	certDir := flags.String("cert-dir", "", "directory for mTLS certificates; defaults to <config-dir>/certs")
	port := flags.String("port", "7270", "master HTTPS port")
	cleanupInterval := flags.String("cleanup-interval", "15s", "registry cleanup interval")
	minWorkerCPUFree := flags.Float64("min-worker-cpu-free", 0, "minimum free CPU percent required for scheduling")
	minWorkerRAMFreeMB := flags.Float64("min-worker-ram-free-mb", 0, "minimum free RAM in MiB required for scheduling")
	executeClientAllowlist := flags.String("execute-client-allowlist", "", "comma-separated client certificate CN or DNS SAN values allowed to call /api/v1/execute")
	maxExecuteBodyBytes := flags.Int64("max-execution-request-bytes", 2<<20, "maximum JSON body size accepted by /api/v1/execute")
	devWorkerID := flags.String("dev-worker-id", "worker-vn-01", "worker ID used when generating development certificates")
	devCerts := flags.Bool("dev-certs", false, "generate local development certificates")
	force := flags.Bool("force", false, "overwrite existing generated files")

	if err := flags.Parse(args); err != nil {
		return err
	}

	result, err := bootstrap.InitMaster(bootstrap.MasterOptions{
		ConfigDir:           *configDir,
		CertDir:             *certDir,
		Port:                *port,
		CleanupInterval:     *cleanupInterval,
		MinWorkerCPUFree:    *minWorkerCPUFree,
		MinWorkerRAMFreeMB:  *minWorkerRAMFreeMB,
		ExecuteClientIDs:    *executeClientAllowlist,
		MaxExecuteBodyBytes: *maxExecuteBodyBytes,
		DevWorkerID:         *devWorkerID,
		GenerateDevCerts:    *devCerts,
		Force:               *force,
	})
	if err != nil {
		return err
	}

	fmt.Printf("created master config: %s\n", result.ConfigPath)
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
