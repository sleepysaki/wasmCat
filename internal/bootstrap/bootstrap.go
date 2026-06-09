package bootstrap

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"wasmcat/internal/security"
)

type MasterOptions struct {
	ConfigDir           string
	CertDir             string
	Port                string
	CleanupInterval     string
	WorkerStaleTimeout  string
	ShutdownTimeout     string
	MinWorkerCPUFree    float64
	MinWorkerRAMFreeMB  float64
	ExecuteClientIDs    string
	MaxExecuteBodyBytes int64
	DevWorkerID         string
	GenerateDevCerts    bool
	Force               bool
}

type WorkerOptions struct {
	ConfigDir          string
	CertDir            string
	WorkerID           string
	Port               string
	MasterURL          string
	AdvertiseAddress   string
	Latitude           float64
	Longitude          float64
	HeartbeatInterval  string
	ExecutionTimeout   string
	ModuleFetchTimeout string
	ShutdownTimeout    string
	MaxModuleBytes     int64
	MaxPayloadBytes    int64
	MaxOutputBytes     uint32
	MaxConcurrentExecs int
	MaxCachedModules   int
	MaxCacheBytes      int64
	ModuleCacheTTL     string
	Force              bool
}

type Result struct {
	ConfigPath string
	CertDir    string
	Warnings   []string
}

func InitMaster(options MasterOptions) (Result, error) {
	options = normalizeMaster(options)
	if err := validateMaster(options); err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(options.ConfigDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create config directory: %w", err)
	}
	if err := os.MkdirAll(options.CertDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create cert directory: %w", err)
	}

	if options.GenerateDevCerts {
		if err := ensureDevCertTargetsCanBeWritten(options.CertDir, options.DevWorkerID, options.Force); err != nil {
			return Result{}, err
		}
		if err := security.GenerateCAAndCerts(options.CertDir, options.DevWorkerID); err != nil {
			return Result{}, err
		}
	}

	configPath := filepath.Join(options.ConfigDir, "master.env")
	values := []envValue{
		{"MASTER_PORT", options.Port},
		{"CERT_DIR", options.CertDir},
		{"AUTO_GENERATE_CERTS", "false"},
		{"DEV_WORKER_ID", options.DevWorkerID},
		{"CLEANUP_INTERVAL", options.CleanupInterval},
		{"WORKER_STALE_TIMEOUT", options.WorkerStaleTimeout},
		{"MASTER_SHUTDOWN_TIMEOUT", options.ShutdownTimeout},
		{"MIN_WORKER_CPU_FREE", strconv.FormatFloat(options.MinWorkerCPUFree, 'f', -1, 64)},
		{"MIN_WORKER_RAM_FREE_MB", strconv.FormatFloat(options.MinWorkerRAMFreeMB, 'f', -1, 64)},
		{"EXECUTE_CLIENT_ALLOWLIST", options.ExecuteClientIDs},
		{"MAX_EXECUTION_REQUEST_BYTES", strconv.FormatInt(options.MaxExecuteBodyBytes, 10)},
	}

	if err := writeEnvFile(configPath, values, options.Force); err != nil {
		return Result{}, err
	}

	result := Result{ConfigPath: configPath, CertDir: options.CertDir}
	if !options.GenerateDevCerts {
		result.Warnings = append(result.Warnings, "copy ca.crt, master.crt, and master.key into the cert directory before starting the master")
	}

	return result, nil
}

func InitWorker(options WorkerOptions) (Result, error) {
	options = normalizeWorker(options)
	if err := validateWorker(options); err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(options.ConfigDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create config directory: %w", err)
	}
	if err := os.MkdirAll(options.CertDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create cert directory: %w", err)
	}

	configPath := filepath.Join(options.ConfigDir, "worker.env")
	values := []envValue{
		{"WORKER_ID", options.WorkerID},
		{"WORKER_PORT", options.Port},
		{"MASTER_URL", options.MasterURL},
		{"WORKER_ADVERTISE_ADDRESS", options.AdvertiseAddress},
		{"WORKER_LATITUDE", strconv.FormatFloat(options.Latitude, 'f', -1, 64)},
		{"WORKER_LONGITUDE", strconv.FormatFloat(options.Longitude, 'f', -1, 64)},
		{"CERT_DIR", options.CertDir},
		{"HEARTBEAT_INTERVAL", options.HeartbeatInterval},
		{"EXECUTION_TIMEOUT", options.ExecutionTimeout},
		{"MODULE_FETCH_TIMEOUT", options.ModuleFetchTimeout},
		{"WORKER_SHUTDOWN_TIMEOUT", options.ShutdownTimeout},
		{"MAX_MODULE_BYTES", strconv.FormatInt(options.MaxModuleBytes, 10)},
		{"MAX_PAYLOAD_BYTES", strconv.FormatInt(options.MaxPayloadBytes, 10)},
		{"MAX_OUTPUT_BYTES", strconv.FormatUint(uint64(options.MaxOutputBytes), 10)},
		{"MAX_CONCURRENT_EXECS", strconv.Itoa(options.MaxConcurrentExecs)},
		{"MAX_CACHED_MODULES", strconv.Itoa(options.MaxCachedModules)},
		{"MAX_CACHE_BYTES", strconv.FormatInt(options.MaxCacheBytes, 10)},
		{"MODULE_CACHE_TTL", options.ModuleCacheTTL},
	}

	if err := writeEnvFile(configPath, values, options.Force); err != nil {
		return Result{}, err
	}

	return Result{
		ConfigPath: configPath,
		CertDir:    options.CertDir,
		Warnings: []string{
			fmt.Sprintf("copy ca.crt, worker-%s.crt, and worker-%s.key into the cert directory before starting the worker", options.WorkerID, options.WorkerID),
		},
	}, nil
}

func normalizeMaster(options MasterOptions) MasterOptions {
	if options.Port == "" {
		options.Port = "7270"
	}
	if options.CleanupInterval == "" {
		options.CleanupInterval = "15s"
	}
	if options.WorkerStaleTimeout == "" {
		options.WorkerStaleTimeout = "30s"
	}
	if options.ShutdownTimeout == "" {
		options.ShutdownTimeout = "5s"
	}
	if options.DevWorkerID == "" {
		options.DevWorkerID = "worker-vn-01"
	}
	if options.CertDir == "" && options.ConfigDir != "" {
		options.CertDir = filepath.Join(options.ConfigDir, "certs")
	}
	if options.MaxExecuteBodyBytes == 0 {
		options.MaxExecuteBodyBytes = 2 << 20
	}

	return options
}

func normalizeWorker(options WorkerOptions) WorkerOptions {
	if options.Port == "" {
		options.Port = "7271"
	}
	if options.WorkerID == "" {
		options.WorkerID = "worker-vn-01"
	}
	if options.MasterURL == "" {
		options.MasterURL = "https://localhost:7270"
	}
	if options.AdvertiseAddress == "" {
		options.AdvertiseAddress = "localhost:" + options.Port
	}
	if options.HeartbeatInterval == "" {
		options.HeartbeatInterval = "5s"
	}
	if options.ExecutionTimeout == "" {
		options.ExecutionTimeout = "5s"
	}
	if options.ModuleFetchTimeout == "" {
		options.ModuleFetchTimeout = "10s"
	}
	if options.ShutdownTimeout == "" {
		options.ShutdownTimeout = "10s"
	}
	if options.ModuleCacheTTL == "" {
		options.ModuleCacheTTL = "30m"
	}
	if options.CertDir == "" && options.ConfigDir != "" {
		options.CertDir = filepath.Join(options.ConfigDir, "certs")
	}

	return options
}

func validateMaster(options MasterOptions) error {
	if strings.TrimSpace(options.ConfigDir) == "" {
		return fmt.Errorf("config directory is required")
	}
	if strings.TrimSpace(options.CertDir) == "" {
		return fmt.Errorf("cert directory is required")
	}
	if strings.TrimSpace(options.Port) == "" {
		return fmt.Errorf("master port is required")
	}
	if strings.TrimSpace(options.DevWorkerID) == "" {
		return fmt.Errorf("dev worker id is required")
	}
	if err := validatePositiveDuration("cleanup interval", options.CleanupInterval); err != nil {
		return fmt.Errorf("parse cleanup interval: %w", err)
	}
	if err := validatePositiveDuration("worker stale timeout", options.WorkerStaleTimeout); err != nil {
		return fmt.Errorf("parse worker stale timeout: %w", err)
	}
	if err := validatePositiveDuration("master shutdown timeout", options.ShutdownTimeout); err != nil {
		return fmt.Errorf("parse master shutdown timeout: %w", err)
	}
	if options.MinWorkerCPUFree < 0 || options.MinWorkerCPUFree > 100 {
		return fmt.Errorf("min worker cpu free must be between 0 and 100")
	}
	if options.MinWorkerRAMFreeMB < 0 {
		return fmt.Errorf("min worker ram free mb must be greater than or equal to zero")
	}
	if options.MaxExecuteBodyBytes <= 0 {
		return fmt.Errorf("max execution request bytes must be greater than zero")
	}

	return nil
}

func validateWorker(options WorkerOptions) error {
	if strings.TrimSpace(options.ConfigDir) == "" {
		return fmt.Errorf("config directory is required")
	}
	if strings.TrimSpace(options.CertDir) == "" {
		return fmt.Errorf("cert directory is required")
	}
	if strings.TrimSpace(options.WorkerID) == "" {
		return fmt.Errorf("worker id is required")
	}
	if strings.TrimSpace(options.Port) == "" {
		return fmt.Errorf("worker port is required")
	}
	if err := validateURL(options.MasterURL); err != nil {
		return err
	}
	if strings.TrimSpace(options.AdvertiseAddress) == "" {
		return fmt.Errorf("worker advertise address is required")
	}
	if options.Latitude < -90 || options.Latitude > 90 {
		return fmt.Errorf("worker latitude must be between -90 and 90")
	}
	if options.Longitude < -180 || options.Longitude > 180 {
		return fmt.Errorf("worker longitude must be between -180 and 180")
	}
	if err := validatePositiveDuration("heartbeat interval", options.HeartbeatInterval); err != nil {
		return fmt.Errorf("parse heartbeat interval: %w", err)
	}
	if err := validatePositiveDuration("execution timeout", options.ExecutionTimeout); err != nil {
		return fmt.Errorf("parse execution timeout: %w", err)
	}
	if err := validatePositiveDuration("module fetch timeout", options.ModuleFetchTimeout); err != nil {
		return fmt.Errorf("parse module fetch timeout: %w", err)
	}
	if err := validatePositiveDuration("worker shutdown timeout", options.ShutdownTimeout); err != nil {
		return fmt.Errorf("parse worker shutdown timeout: %w", err)
	}
	if err := validatePositiveDuration("module cache ttl", options.ModuleCacheTTL); err != nil {
		return fmt.Errorf("parse module cache ttl: %w", err)
	}
	if options.MaxModuleBytes <= 0 {
		return fmt.Errorf("max module bytes must be greater than zero")
	}
	if options.MaxPayloadBytes <= 0 {
		return fmt.Errorf("max payload bytes must be greater than zero")
	}
	if options.MaxOutputBytes == 0 {
		return fmt.Errorf("max output bytes must be greater than zero")
	}
	if options.MaxConcurrentExecs <= 0 {
		return fmt.Errorf("max concurrent executions must be greater than zero")
	}
	if options.MaxCachedModules <= 0 {
		return fmt.Errorf("max cached modules must be greater than zero")
	}
	if options.MaxCacheBytes <= 0 {
		return fmt.Errorf("max cache bytes must be greater than zero")
	}

	return nil
}

func validatePositiveDuration(label string, value string) error {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	if parsed <= 0 {
		return fmt.Errorf("%s must be greater than zero", label)
	}

	return nil
}

func validateURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse master url: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("master url must be an https URL with a host")
	}

	return nil
}

func ensureDevCertTargetsCanBeWritten(certDir string, workerID string, force bool) error {
	if force {
		return nil
	}

	paths := []string{
		security.CACertPath(certDir),
		filepath.Join(certDir, "ca.key"),
		security.MasterCertPath(certDir),
		security.MasterKeyPath(certDir),
		security.WorkerCertPath(certDir, workerID),
		security.WorkerKeyPath(certDir, workerID),
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite development certificates", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check %s: %w", path, err)
		}
	}

	return nil
}

type envValue struct {
	Name  string
	Value string
}

func writeEnvFile(path string, values []envValue, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite it", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check %s: %w", path, err)
		}
	}

	var builder strings.Builder
	for _, value := range values {
		builder.WriteString(value.Name)
		builder.WriteString("=")
		builder.WriteString(value.Value)
		builder.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	return nil
}
