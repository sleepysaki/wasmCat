package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"wasmcat/internal/logging"
	"wasmcat/internal/shared"
	"wasmcat/internal/ui"
)

var version = "dev"

func main() {
	listen := flag.String("listen", ui.DefaultListenAddress, "HTTP listen address for the operator dashboard")
	configPath := flag.String("config", "", "wasmcatctl-compatible config file path")
	timeout := flag.Duration("timeout", ui.DefaultAPITimeout, "timeout for master API calls")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wasmcat-ui %s\n", version)
		return
	}

	logging.Configure("ui")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := shared.NewHTTPServer(*listen, (&ui.Server{
		ConfigPath: *configPath,
		Timeout:    *timeout,
	}).Handler(), nil)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("wasmcat ui live", "listen", *listen)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("wasmcat ui crashed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("wasmcat ui shutdown failed", "error", err)
			os.Exit(1)
		}
		slog.Info("wasmcat ui stopped")
	}
}
