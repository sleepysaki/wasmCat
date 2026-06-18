package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"wasmcat/internal/geolocation"
	"wasmcat/internal/shared"
)

type Limits struct {
	ExecutionTimeout   time.Duration
	ModuleFetchTimeout time.Duration
	ShutdownTimeout    time.Duration
	MaxModuleBytes     int64
	MaxPayloadBytes    int64
	MaxOutputBytes     uint32
	MaxConcurrentExecs int
	MaxCachedModules   int
	MaxCacheBytes      int64
	ModuleCacheTTL     time.Duration
}

type MasterConfig struct {
	Port                   string
	CertDir                string
	AutoGenerateCerts      bool
	WorkerIDForCert        string
	CleanupInterval        time.Duration
	WorkerStaleTimeout     time.Duration
	ShutdownTimeout        time.Duration
	MinWorkerCPUFree       float64
	MinWorkerRAMFreeMB     float64
	ExecuteClientIDs       []string
	MaxExecuteBodyBytes    int64
	RequestCacheTTL        time.Duration
	RequestCacheMaxEntries int
	JobStorePath           string
	JobMaxAttempts         int
	JobLeaseTTL            time.Duration
	JobRecoveryInterval    time.Duration
	JobRecoveryBatchSize   int
	ModuleHostAllowlist    []string
	RequireModuleDigest    bool
}

type WorkerConfig struct {
	Port                string
	NodeID              string
	MasterURL           string
	AdvertiseAddress    string
	Latitude            float64
	Longitude           float64
	LocationSource      string
	AutoDetectLocation  bool
	LocationProviderURL string
	CertDir             string
	HeartbeatInterval   time.Duration
	Limits              Limits
}

func LoadMaster() (MasterConfig, error) {
	cleanupInterval, err := positiveDurationEnv("CLEANUP_INTERVAL", 15*time.Second)
	if err != nil {
		return MasterConfig{}, err
	}
	workerStaleTimeout, err := positiveDurationEnv("WORKER_STALE_TIMEOUT", 30*time.Second)
	if err != nil {
		return MasterConfig{}, err
	}
	shutdownTimeout, err := positiveDurationEnv("MASTER_SHUTDOWN_TIMEOUT", 5*time.Second)
	if err != nil {
		return MasterConfig{}, err
	}
	autoGenerateCerts, err := boolEnv("AUTO_GENERATE_CERTS", true)
	if err != nil {
		return MasterConfig{}, err
	}
	minWorkerCPUFree, err := float64Env("MIN_WORKER_CPU_FREE", 0)
	if err != nil {
		return MasterConfig{}, err
	}
	if minWorkerCPUFree < 0 || minWorkerCPUFree > 100 {
		return MasterConfig{}, fmt.Errorf("MIN_WORKER_CPU_FREE must be between 0 and 100")
	}
	minWorkerRAMFreeMB, err := float64Env("MIN_WORKER_RAM_FREE_MB", 0)
	if err != nil {
		return MasterConfig{}, err
	}
	if minWorkerRAMFreeMB < 0 {
		return MasterConfig{}, fmt.Errorf("MIN_WORKER_RAM_FREE_MB must be greater than or equal to zero")
	}
	maxExecuteBodyBytes, err := positiveInt64Env("MAX_EXECUTION_REQUEST_BYTES", 2<<20)
	if err != nil {
		return MasterConfig{}, err
	}
	requestCacheTTL, err := positiveDurationEnv("EXECUTION_REQUEST_CACHE_TTL", 5*time.Minute)
	if err != nil {
		return MasterConfig{}, err
	}
	requestCacheMaxEntries, err := positiveIntEnv("EXECUTION_REQUEST_CACHE_MAX_ENTRIES", 4096)
	if err != nil {
		return MasterConfig{}, err
	}
	jobMaxAttempts, err := positiveIntEnv("JOB_MAX_ATTEMPTS", 3)
	if err != nil {
		return MasterConfig{}, err
	}
	jobLeaseTTL, err := positiveDurationEnv("JOB_LEASE_TTL", 30*time.Second)
	if err != nil {
		return MasterConfig{}, err
	}
	jobRecoveryInterval, err := positiveDurationEnv("JOB_RECOVERY_INTERVAL", 5*time.Second)
	if err != nil {
		return MasterConfig{}, err
	}
	jobRecoveryBatchSize, err := positiveIntEnv("JOB_RECOVERY_BATCH_SIZE", 32)
	if err != nil {
		return MasterConfig{}, err
	}
	requireModuleDigest, err := boolEnv("REQUIRE_MODULE_DIGEST", false)
	if err != nil {
		return MasterConfig{}, err
	}

	cfg := MasterConfig{
		Port:                   stringEnv("MASTER_PORT", "7270"),
		CertDir:                stringEnv("CERT_DIR", "./certs"),
		AutoGenerateCerts:      autoGenerateCerts,
		WorkerIDForCert:        stringEnv("DEV_WORKER_ID", "worker-vn-01"),
		CleanupInterval:        cleanupInterval,
		WorkerStaleTimeout:     workerStaleTimeout,
		ShutdownTimeout:        shutdownTimeout,
		MinWorkerCPUFree:       minWorkerCPUFree,
		MinWorkerRAMFreeMB:     minWorkerRAMFreeMB,
		ExecuteClientIDs:       listEnv("EXECUTE_CLIENT_ALLOWLIST"),
		MaxExecuteBodyBytes:    maxExecuteBodyBytes,
		RequestCacheTTL:        requestCacheTTL,
		RequestCacheMaxEntries: requestCacheMaxEntries,
		JobStorePath:           stringEnv("JOB_STORE_PATH", "./wasmcat-jobs.db"),
		JobMaxAttempts:         jobMaxAttempts,
		JobLeaseTTL:            jobLeaseTTL,
		JobRecoveryInterval:    jobRecoveryInterval,
		JobRecoveryBatchSize:   jobRecoveryBatchSize,
		ModuleHostAllowlist:    listEnv("MODULE_HOST_ALLOWLIST"),
		RequireModuleDigest:    requireModuleDigest,
	}

	return cfg, nil
}

func listEnv(name string) []string {
	value := os.Getenv(name)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}

	return values
}

func LoadWorker() (WorkerConfig, error) {
	heartbeatInterval, err := positiveDurationEnv("HEARTBEAT_INTERVAL", 5*time.Second)
	if err != nil {
		return WorkerConfig{}, err
	}

	limits, err := loadWorkerLimits()
	if err != nil {
		return WorkerConfig{}, err
	}
	autoDetectLocation, err := boolEnv("WORKER_AUTO_DETECT_LOCATION", true)
	if err != nil {
		return WorkerConfig{}, err
	}
	locationProviderURL := stringEnv("WORKER_LOCATION_PROVIDER_URL", geolocation.DefaultProviderURL)

	latitude, longitude, locationSource, err := loadWorkerCoordinates()
	if err != nil {
		return WorkerConfig{}, err
	}
	if locationSource == "" && autoDetectLocation {
		locationSource = "auto_pending"
	} else if locationSource == "" {
		locationSource = "unset"
	}

	nodeID := stringEnv("WORKER_ID", "worker-vn-01")
	port := stringEnv("WORKER_PORT", "7271")

	cfg := WorkerConfig{
		Port:                port,
		NodeID:              nodeID,
		MasterURL:           stringEnv("MASTER_URL", "https://localhost:7270"),
		AdvertiseAddress:    stringEnv("WORKER_ADVERTISE_ADDRESS", shared.DefaultWorkerAdvertiseAddress(port)),
		Latitude:            latitude,
		Longitude:           longitude,
		LocationSource:      locationSource,
		AutoDetectLocation:  autoDetectLocation,
		LocationProviderURL: locationProviderURL,
		CertDir:             stringEnv("CERT_DIR", "./certs"),
		HeartbeatInterval:   heartbeatInterval,
		Limits:              limits,
	}

	return cfg, nil
}

func ResolveWorkerLocation(ctx context.Context, cfg WorkerConfig) (WorkerConfig, error) {
	switch cfg.LocationSource {
	case "env":
		if err := geolocation.Validate(cfg.Latitude, cfg.Longitude, "worker"); err != nil {
			return WorkerConfig{}, err
		}
		return cfg, nil
	case "auto_pending", "":
		if !cfg.AutoDetectLocation {
			return WorkerConfig{}, fmt.Errorf("worker location is not configured; set WORKER_LATITUDE/WORKER_LONGITUDE or enable WORKER_AUTO_DETECT_LOCATION")
		}
		detected, err := geolocation.Detect(ctx, cfg.LocationProviderURL)
		if err != nil {
			return WorkerConfig{}, fmt.Errorf("auto-detect worker location: %w", err)
		}
		cfg.Latitude = detected.Latitude
		cfg.Longitude = detected.Longitude
		cfg.LocationSource = detected.Source
		return cfg, nil
	case "unset":
		return WorkerConfig{}, fmt.Errorf("worker location is not configured; set WORKER_LATITUDE/WORKER_LONGITUDE or enable WORKER_AUTO_DETECT_LOCATION")
	default:
		return WorkerConfig{}, fmt.Errorf("unknown worker location source %q", cfg.LocationSource)
	}
}

func loadWorkerCoordinates() (float64, float64, string, error) {
	latValue, hasLat := os.LookupEnv("WORKER_LATITUDE")
	lonValue, hasLon := os.LookupEnv("WORKER_LONGITUDE")
	if hasLat != hasLon {
		return 0, 0, "", fmt.Errorf("WORKER_LATITUDE and WORKER_LONGITUDE must be set together")
	}
	if !hasLat {
		return 0, 0, "", nil
	}

	latitude, err := strconv.ParseFloat(strings.TrimSpace(latValue), 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("parse WORKER_LATITUDE: %w", err)
	}
	longitude, err := strconv.ParseFloat(strings.TrimSpace(lonValue), 64)
	if err != nil {
		return 0, 0, "", fmt.Errorf("parse WORKER_LONGITUDE: %w", err)
	}
	if err := geolocation.Validate(latitude, longitude, "worker"); err != nil {
		return 0, 0, "", err
	}

	return latitude, longitude, "env", nil
}

func loadWorkerLimits() (Limits, error) {
	defaults := Limits{
		ExecutionTimeout:   5 * time.Second,
		ModuleFetchTimeout: 10 * time.Second,
		ShutdownTimeout:    10 * time.Second,
		MaxModuleBytes:     10 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 4,
		MaxCachedModules:   128,
		MaxCacheBytes:      256 << 20,
		ModuleCacheTTL:     30 * time.Minute,
	}

	executionTimeout, err := positiveDurationEnv("EXECUTION_TIMEOUT", defaults.ExecutionTimeout)
	if err != nil {
		return Limits{}, err
	}
	moduleFetchTimeout, err := positiveDurationEnv("MODULE_FETCH_TIMEOUT", defaults.ModuleFetchTimeout)
	if err != nil {
		return Limits{}, err
	}
	shutdownTimeout, err := positiveDurationEnv("WORKER_SHUTDOWN_TIMEOUT", executionTimeout+5*time.Second)
	if err != nil {
		return Limits{}, err
	}
	maxModuleBytes, err := positiveInt64Env("MAX_MODULE_BYTES", defaults.MaxModuleBytes)
	if err != nil {
		return Limits{}, err
	}
	maxPayloadBytes, err := positiveInt64Env("MAX_PAYLOAD_BYTES", defaults.MaxPayloadBytes)
	if err != nil {
		return Limits{}, err
	}
	maxOutputBytes, err := positiveUint32Env("MAX_OUTPUT_BYTES", defaults.MaxOutputBytes)
	if err != nil {
		return Limits{}, err
	}
	maxConcurrentExecs, err := positiveIntEnv("MAX_CONCURRENT_EXECS", defaults.MaxConcurrentExecs)
	if err != nil {
		return Limits{}, err
	}
	maxCachedModules, err := positiveIntEnv("MAX_CACHED_MODULES", defaults.MaxCachedModules)
	if err != nil {
		return Limits{}, err
	}
	maxCacheBytes, err := positiveInt64Env("MAX_CACHE_BYTES", defaults.MaxCacheBytes)
	if err != nil {
		return Limits{}, err
	}
	moduleCacheTTL, err := positiveDurationEnv("MODULE_CACHE_TTL", defaults.ModuleCacheTTL)
	if err != nil {
		return Limits{}, err
	}

	return Limits{
		ExecutionTimeout:   executionTimeout,
		ModuleFetchTimeout: moduleFetchTimeout,
		ShutdownTimeout:    shutdownTimeout,
		MaxModuleBytes:     maxModuleBytes,
		MaxPayloadBytes:    maxPayloadBytes,
		MaxOutputBytes:     maxOutputBytes,
		MaxConcurrentExecs: maxConcurrentExecs,
		MaxCachedModules:   maxCachedModules,
		MaxCacheBytes:      maxCacheBytes,
		ModuleCacheTTL:     moduleCacheTTL,
	}, nil
}

func stringEnv(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s duration: %w", name, err)
	}

	return parsed, nil
}

func positiveDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	parsed, err := durationEnv(name, fallback)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return parsed, nil
}

func boolEnv(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s boolean: %w", name, err)
	}

	return parsed, nil
}

func intEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s integer: %w", name, err)
	}

	return parsed, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	parsed, err := intEnv(name, fallback)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return parsed, nil
}

func int64Env(name string, fallback int64) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s integer: %w", name, err)
	}

	return parsed, nil
}

func positiveInt64Env(name string, fallback int64) (int64, error) {
	parsed, err := int64Env(name, fallback)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return parsed, nil
}

func float64Env(name string, fallback float64) (float64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s float: %w", name, err)
	}

	return parsed, nil
}

func uint32Env(name string, fallback uint32) (uint32, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s integer: %w", name, err)
	}

	return uint32(parsed), nil
}

func positiveUint32Env(name string, fallback uint32) (uint32, error) {
	parsed, err := uint32Env(name, fallback)
	if err != nil {
		return 0, err
	}
	if parsed == 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return parsed, nil
}
