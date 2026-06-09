package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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
	Port                string
	CertDir             string
	AutoGenerateCerts   bool
	WorkerIDForCert     string
	CleanupInterval     time.Duration
	WorkerStaleTimeout  time.Duration
	ShutdownTimeout     time.Duration
	MinWorkerCPUFree    float64
	MinWorkerRAMFreeMB  float64
	ExecuteClientIDs    []string
	MaxExecuteBodyBytes int64
}

type WorkerConfig struct {
	Port              string
	NodeID            string
	MasterURL         string
	AdvertiseAddress  string
	Latitude          float64
	Longitude         float64
	CertDir           string
	HeartbeatInterval time.Duration
	Limits            Limits
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
	minWorkerRAMFreeMB, err := float64Env("MIN_WORKER_RAM_FREE_MB", 0)
	if err != nil {
		return MasterConfig{}, err
	}
	maxExecuteBodyBytes, err := int64Env("MAX_EXECUTION_REQUEST_BYTES", 2<<20)
	if err != nil {
		return MasterConfig{}, err
	}

	cfg := MasterConfig{
		Port:                stringEnv("MASTER_PORT", "7270"),
		CertDir:             stringEnv("CERT_DIR", "./certs"),
		AutoGenerateCerts:   autoGenerateCerts,
		WorkerIDForCert:     stringEnv("DEV_WORKER_ID", "worker-vn-01"),
		CleanupInterval:     cleanupInterval,
		WorkerStaleTimeout:  workerStaleTimeout,
		ShutdownTimeout:     shutdownTimeout,
		MinWorkerCPUFree:    minWorkerCPUFree,
		MinWorkerRAMFreeMB:  minWorkerRAMFreeMB,
		ExecuteClientIDs:    listEnv("EXECUTE_CLIENT_ALLOWLIST"),
		MaxExecuteBodyBytes: maxExecuteBodyBytes,
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
	latitude, err := float64Env("WORKER_LATITUDE", 0)
	if err != nil {
		return WorkerConfig{}, err
	}
	if latitude < -90 || latitude > 90 {
		return WorkerConfig{}, fmt.Errorf("WORKER_LATITUDE must be between -90 and 90")
	}
	longitude, err := float64Env("WORKER_LONGITUDE", 0)
	if err != nil {
		return WorkerConfig{}, err
	}
	if longitude < -180 || longitude > 180 {
		return WorkerConfig{}, fmt.Errorf("WORKER_LONGITUDE must be between -180 and 180")
	}

	nodeID := stringEnv("WORKER_ID", "worker-vn-01")
	port := stringEnv("WORKER_PORT", "7271")

	cfg := WorkerConfig{
		Port:              port,
		NodeID:            nodeID,
		MasterURL:         stringEnv("MASTER_URL", "https://localhost:7270"),
		AdvertiseAddress:  stringEnv("WORKER_ADVERTISE_ADDRESS", "localhost:"+port),
		Latitude:          latitude,
		Longitude:         longitude,
		CertDir:           stringEnv("CERT_DIR", "./certs"),
		HeartbeatInterval: heartbeatInterval,
		Limits:            limits,
	}

	return cfg, nil
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
	maxModuleBytes, err := int64Env("MAX_MODULE_BYTES", defaults.MaxModuleBytes)
	if err != nil {
		return Limits{}, err
	}
	maxPayloadBytes, err := int64Env("MAX_PAYLOAD_BYTES", defaults.MaxPayloadBytes)
	if err != nil {
		return Limits{}, err
	}
	maxOutputBytes, err := uint32Env("MAX_OUTPUT_BYTES", defaults.MaxOutputBytes)
	if err != nil {
		return Limits{}, err
	}
	maxConcurrentExecs, err := intEnv("MAX_CONCURRENT_EXECS", defaults.MaxConcurrentExecs)
	if err != nil {
		return Limits{}, err
	}
	maxCachedModules, err := intEnv("MAX_CACHED_MODULES", defaults.MaxCachedModules)
	if err != nil {
		return Limits{}, err
	}
	maxCacheBytes, err := int64Env("MAX_CACHE_BYTES", defaults.MaxCacheBytes)
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
