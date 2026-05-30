package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Limits struct {
	ExecutionTimeout   time.Duration
	ModuleFetchTimeout time.Duration
	MaxModuleBytes     int64
	MaxPayloadBytes    int64
	MaxOutputBytes     uint32
	MaxConcurrentExecs int
}

type MasterConfig struct {
	Port            string
	WorkerIDForCert string
	CleanupInterval time.Duration
}

type WorkerConfig struct {
	Port              string
	NodeID            string
	MasterURL         string
	AdvertiseAddress  string
	HeartbeatInterval time.Duration
	Limits            Limits
}

func LoadMaster() (MasterConfig, error) {
	cleanupInterval, err := durationEnv("CLEANUP_INTERVAL", 15*time.Second)
	if err != nil {
		return MasterConfig{}, err
	}

	cfg := MasterConfig{
		Port:            stringEnv("MASTER_PORT", "7270"),
		WorkerIDForCert: stringEnv("DEV_WORKER_ID", "worker-vn-01"),
		CleanupInterval: cleanupInterval,
	}

	return cfg, nil
}

func LoadWorker() (WorkerConfig, error) {
	heartbeatInterval, err := durationEnv("HEARTBEAT_INTERVAL", 5*time.Second)
	if err != nil {
		return WorkerConfig{}, err
	}

	limits, err := loadWorkerLimits()
	if err != nil {
		return WorkerConfig{}, err
	}

	nodeID := stringEnv("WORKER_ID", "worker-vn-01")
	port := stringEnv("WORKER_PORT", "7271")

	cfg := WorkerConfig{
		Port:              port,
		NodeID:            nodeID,
		MasterURL:         stringEnv("MASTER_URL", "https://localhost:7270"),
		AdvertiseAddress:  stringEnv("WORKER_ADVERTISE_ADDRESS", "localhost:"+port),
		HeartbeatInterval: heartbeatInterval,
		Limits:            limits,
	}

	return cfg, nil
}

func loadWorkerLimits() (Limits, error) {
	defaults := Limits{
		ExecutionTimeout:   5 * time.Second,
		ModuleFetchTimeout: 10 * time.Second,
		MaxModuleBytes:     10 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 4,
	}

	executionTimeout, err := durationEnv("EXECUTION_TIMEOUT", defaults.ExecutionTimeout)
	if err != nil {
		return Limits{}, err
	}
	moduleFetchTimeout, err := durationEnv("MODULE_FETCH_TIMEOUT", defaults.ModuleFetchTimeout)
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

	return Limits{
		ExecutionTimeout:   executionTimeout,
		ModuleFetchTimeout: moduleFetchTimeout,
		MaxModuleBytes:     maxModuleBytes,
		MaxPayloadBytes:    maxPayloadBytes,
		MaxOutputBytes:     maxOutputBytes,
		MaxConcurrentExecs: maxConcurrentExecs,
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
