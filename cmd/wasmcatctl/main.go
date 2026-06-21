package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"wasmcat/internal/ctl"
	"wasmcat/internal/shared"
)

var version = "dev"

type globalFlags struct {
	configPath string
	masterURL  string
	caCert     string
	clientCert string
	clientKey  string
	output     string
	timeout    time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "wasmcatctl: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return flag.ErrHelp
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return nil
	}
	if args[0] == "version" {
		_, err := fmt.Fprintf(stdout, "wasmcatctl %s\n", version)
		return err
	}
	if args[0] == "config" {
		return runConfig(args[1:], stdout)
	}

	globals, remaining, err := parseGlobalFlags(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return fmt.Errorf("command is required")
	}

	cfg, err := ctl.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	cfg = ctl.ApplyOverrides(cfg, globals.masterURL, globals.caCert, globals.clientCert, globals.clientKey, globals.output, 0, 0, false, false)

	client, err := ctl.NewClient(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globals.timeout)
	defer cancel()

	command := remaining[0]
	switch command {
	case "health":
		response, err := client.Health(ctx)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, cfg.Output, response)
	case "ready":
		response, err := client.Ready(ctx)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, cfg.Output, response)
	case "metrics":
		response, err := client.Metrics(ctx)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, cfg.Output, response)
	case "workers":
		response, err := client.Workers(ctx)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, cfg.Output, response)
	case "execute":
		req, err := parseExecutionFlags(ctx, remaining[1:], cfg)
		if err != nil {
			return err
		}
		response, err := client.Execute(ctx, req)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, cfg.Output, response)
	case "jobs":
		return runJobs(ctx, client, cfg.Output, remaining[1:], stdout)
	case "drain":
		if len(remaining) < 2 {
			return fmt.Errorf("worker_id is required")
		}
		response, err := client.Drain(ctx, remaining[1])
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, cfg.Output, response)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func parseGlobalFlags(args []string) (globalFlags, []string, error) {
	globals := globalFlags{timeout: 30 * time.Second}
	remaining := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remaining = append(remaining, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			remaining = append(remaining, args[i:]...)
			break
		}

		name, value, hasInlineValue := strings.Cut(arg, "=")
		nextValue := func() (string, error) {
			if hasInlineValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}

		switch name {
		case "--config":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			globals.configPath = value
		case "--master":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			globals.masterURL = value
		case "--ca":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			globals.caCert = value
		case "--cert":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			globals.clientCert = value
		case "--key":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			globals.clientKey = value
		case "--output", "-o":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			globals.output = value
		case "--timeout":
			value, err := nextValue()
			if err != nil {
				return globalFlags{}, nil, err
			}
			timeout, err := time.ParseDuration(value)
			if err != nil {
				return globalFlags{}, nil, fmt.Errorf("parse timeout: %w", err)
			}
			globals.timeout = timeout
		default:
			return globalFlags{}, nil, fmt.Errorf("unknown global flag %q", name)
		}
	}
	if globals.timeout <= 0 {
		return globalFlags{}, nil, fmt.Errorf("timeout must be greater than zero")
	}

	return globals, remaining, nil
}

func runConfig(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("config subcommand is required")
	}

	switch args[0] {
	case "path":
		path, err := ctl.DefaultConfigPath()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, path)
		return err
	case "init":
		flags := flag.NewFlagSet("wasmcatctl config init", flag.ContinueOnError)
		configPath := flags.String("config", "", "config file path")
		masterURL := flags.String("master", "", "master URL")
		caCert := flags.String("ca", "", "CA certificate path")
		clientCert := flags.String("cert", "", "client certificate path")
		clientKey := flags.String("key", "", "client private key path")
		output := flags.String("output", "", "default output format: table or json")
		userLat := flags.Float64("user-lat", 0, "default user latitude")
		userLon := flags.Float64("user-lon", 0, "default user longitude")
		autoLocation := flags.Bool("auto-location", true, "auto-detect execution location when coordinates are omitted")
		locationProvider := flags.String("location-provider", "", "HTTP JSON endpoint used for auto location detection")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flagWasPassed(flags, "user-lat") != flagWasPassed(flags, "user-lon") {
			return fmt.Errorf("both --user-lat and --user-lon are required when saving a default execution location")
		}
		cfg := ctl.DefaultLocalConfig()
		cfg = ctl.ApplyOverrides(cfg, *masterURL, *caCert, *clientCert, *clientKey, *output, *userLat, *userLon, flagWasPassed(flags, "user-lat"), flagWasPassed(flags, "user-lon"))
		cfg.AutoDetectLocation = *autoLocation
		if strings.TrimSpace(*locationProvider) != "" {
			cfg.LocationProviderURL = *locationProvider
		}
		if err := cfg.ValidateForRequest(); err != nil {
			return err
		}
		if err := ctl.SaveConfig(*configPath, cfg); err != nil {
			return err
		}
		path := *configPath
		if path == "" {
			defaultPath, err := ctl.DefaultConfigPath()
			if err != nil {
				return err
			}
			path = defaultPath
		}
		_, err := fmt.Fprintf(stdout, "created config: %s\n", path)
		return err
	case "view":
		flags := flag.NewFlagSet("wasmcatctl config view", flag.ContinueOnError)
		configPath := flags.String("config", "", "config file path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := ctl.LoadConfig(*configPath)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, "json", cfg)
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func parseExecutionFlags(ctx context.Context, args []string, cfg ctl.Config) (shared.ExecutionRequest, error) {
	flags := flag.NewFlagSet("wasmcatctl execute", flag.ContinueOnError)
	requestID := flags.String("request-id", "", "stable request ID")
	moduleName := flags.String("module", "", "module name")
	moduleURL := flags.String("url", "", "direct WASM module URL")
	moduleRegistryURL := flags.String("registry-url", "", "ACR module registry URL")
	moduleDigest := flags.String("digest", "", "expected module digest, for example sha256:<hex>")
	payload := flags.String("payload", "", "payload string")
	payloadFile := flags.String("payload-file", "", "file containing payload string")
	userLat := flags.Float64("user-lat", 0, "user latitude; omit to use config or auto-detection")
	userLon := flags.Float64("user-lon", 0, "user longitude; omit to use config or auto-detection")
	if err := flags.Parse(args); err != nil {
		return shared.ExecutionRequest{}, err
	}

	payloadValue, err := readPayload(*payload, *payloadFile)
	if err != nil {
		return shared.ExecutionRequest{}, err
	}

	location, err := ctl.ResolveLocation(ctx, cfg, *userLat, *userLon, flagWasPassed(flags, "user-lat"), flagWasPassed(flags, "user-lon"))
	if err != nil {
		return shared.ExecutionRequest{}, err
	}

	req := shared.ExecutionRequest{
		RequestID:         strings.TrimSpace(*requestID),
		ModuleName:        strings.TrimSpace(*moduleName),
		ModuleURL:         strings.TrimSpace(*moduleURL),
		ModuleRegistryURL: strings.TrimSpace(*moduleRegistryURL),
		ModuleDigest:      strings.TrimSpace(*moduleDigest),
		Payload:           payloadValue,
		UserLat:           location.Latitude,
		UserLon:           location.Longitude,
	}
	if err := req.Validate(); err != nil {
		return shared.ExecutionRequest{}, err
	}

	return req, nil
}

func runJobs(ctx context.Context, client *ctl.Client, output string, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("jobs subcommand is required")
	}

	switch args[0] {
	case "create":
		req, err := parseExecutionFlags(ctx, args[1:], client.Config)
		if err != nil {
			return err
		}
		response, err := client.CreateJob(ctx, req)
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, output, response)
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("request_id is required")
		}
		response, err := client.GetJob(ctx, args[1])
		if err != nil {
			return err
		}
		return ctl.WriteValue(stdout, output, response)
	default:
		return fmt.Errorf("unknown jobs subcommand %q", args[0])
	}
}

func readPayload(payload string, payloadFile string) (string, error) {
	if payload != "" && payloadFile != "" {
		return "", fmt.Errorf("use either --payload or --payload-file, not both")
	}
	if payloadFile == "" {
		return payload, nil
	}

	data, err := os.ReadFile(payloadFile)
	if err != nil {
		return "", fmt.Errorf("read payload file: %w", err)
	}

	return string(data), nil
}

func flagWasPassed(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			found = true
		}
	})

	return found
}

func printUsage(w io.Writer) {
	configPath, err := ctl.DefaultConfigPath()
	if err != nil {
		configPath = "<home>/.wasmcat/config.json"
	}

	fmt.Fprintf(w, `wasmcatctl controls a wasmCat master over mTLS.

Usage:
  wasmcatctl config init [--master URL] [--ca PATH] [--cert PATH] [--key PATH] [--auto-location=true]
  wasmcatctl config view
  wasmcatctl [global flags] health
  wasmcatctl [global flags] ready
  wasmcatctl [global flags] metrics
  wasmcatctl [global flags] workers
  wasmcatctl [global flags] execute --module NAME (--url URL | --registry-url URL) --payload TEXT
  wasmcatctl [global flags] jobs create --module NAME (--url URL | --registry-url URL) --payload TEXT
  wasmcatctl [global flags] jobs get REQUEST_ID
  wasmcatctl [global flags] drain WORKER_ID

Global flags:
  --config PATH   config file path, default %s
  --master URL    override master_url
  --ca PATH       override ca_cert
  --cert PATH     override client_cert
  --key PATH      override client_key
  -o, --output    table or json
  --timeout       request timeout, default 30s

Execution location:
  execute and jobs create use --user-lat/--user-lon when provided, then saved
  default coordinates, then auto-detection from the configured location provider.

`, configPath)
}
