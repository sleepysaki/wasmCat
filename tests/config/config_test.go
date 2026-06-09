package config_test

import (
	"testing"
	"time"
	"wasmcat/internal/config"
)

func TestLoadWorkerReadsEnvironment(t *testing.T) {
	t.Setenv("WORKER_ID", "worker-test")
	t.Setenv("WORKER_PORT", "9001")
	t.Setenv("MASTER_URL", "https://master.test:9443")
	t.Setenv("WORKER_ADVERTISE_ADDRESS", "worker.test:9001")
	t.Setenv("WORKER_LATITUDE", "13.7563")
	t.Setenv("WORKER_LONGITUDE", "100.5018")
	t.Setenv("CERT_DIR", "C:\\wasmcat\\certs")
	t.Setenv("HEARTBEAT_INTERVAL", "2s")
	t.Setenv("EXECUTION_TIMEOUT", "3s")
	t.Setenv("MODULE_FETCH_TIMEOUT", "4s")
	t.Setenv("WORKER_SHUTDOWN_TIMEOUT", "6s")
	t.Setenv("MAX_MODULE_BYTES", "100")
	t.Setenv("MAX_PAYLOAD_BYTES", "50")
	t.Setenv("MAX_OUTPUT_BYTES", "25")
	t.Setenv("MAX_CONCURRENT_EXECS", "2")
	t.Setenv("MAX_CACHED_MODULES", "3")
	t.Setenv("MAX_CACHE_BYTES", "1000")
	t.Setenv("MODULE_CACHE_TTL", "5m")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker returned error: %v", err)
	}

	if cfg.NodeID != "worker-test" {
		t.Fatalf("expected worker-test node ID, got %q", cfg.NodeID)
	}
	if cfg.Port != "9001" {
		t.Fatalf("expected port 9001, got %q", cfg.Port)
	}
	if cfg.MasterURL != "https://master.test:9443" {
		t.Fatalf("expected configured master URL, got %q", cfg.MasterURL)
	}
	if cfg.AdvertiseAddress != "worker.test:9001" {
		t.Fatalf("expected configured advertise address, got %q", cfg.AdvertiseAddress)
	}
	if cfg.Latitude != 13.7563 {
		t.Fatalf("expected configured latitude, got %f", cfg.Latitude)
	}
	if cfg.Longitude != 100.5018 {
		t.Fatalf("expected configured longitude, got %f", cfg.Longitude)
	}
	if cfg.CertDir != "C:\\wasmcat\\certs" {
		t.Fatalf("expected configured cert dir, got %q", cfg.CertDir)
	}
	if cfg.HeartbeatInterval != 2*time.Second {
		t.Fatalf("expected heartbeat interval 2s, got %s", cfg.HeartbeatInterval)
	}
	if cfg.Limits.ExecutionTimeout != 3*time.Second {
		t.Fatalf("expected execution timeout 3s, got %s", cfg.Limits.ExecutionTimeout)
	}
	if cfg.Limits.ModuleFetchTimeout != 4*time.Second {
		t.Fatalf("expected module fetch timeout 4s, got %s", cfg.Limits.ModuleFetchTimeout)
	}
	if cfg.Limits.ShutdownTimeout != 6*time.Second {
		t.Fatalf("expected shutdown timeout 6s, got %s", cfg.Limits.ShutdownTimeout)
	}
	if cfg.Limits.MaxModuleBytes != 100 {
		t.Fatalf("expected max module bytes 100, got %d", cfg.Limits.MaxModuleBytes)
	}
	if cfg.Limits.MaxPayloadBytes != 50 {
		t.Fatalf("expected max payload bytes 50, got %d", cfg.Limits.MaxPayloadBytes)
	}
	if cfg.Limits.MaxOutputBytes != 25 {
		t.Fatalf("expected max output bytes 25, got %d", cfg.Limits.MaxOutputBytes)
	}
	if cfg.Limits.MaxConcurrentExecs != 2 {
		t.Fatalf("expected max concurrent execs 2, got %d", cfg.Limits.MaxConcurrentExecs)
	}
	if cfg.Limits.MaxCachedModules != 3 {
		t.Fatalf("expected max cached modules 3, got %d", cfg.Limits.MaxCachedModules)
	}
	if cfg.Limits.MaxCacheBytes != 1000 {
		t.Fatalf("expected max cache bytes 1000, got %d", cfg.Limits.MaxCacheBytes)
	}
	if cfg.Limits.ModuleCacheTTL != 5*time.Minute {
		t.Fatalf("expected module cache ttl 5m, got %s", cfg.Limits.ModuleCacheTTL)
	}
}

func TestLoadMasterRejectsInvalidDuration(t *testing.T) {
	t.Setenv("CLEANUP_INTERVAL", "not-a-duration")

	_, err := config.LoadMaster()
	if err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestLoadMasterRejectsNonPositiveDuration(t *testing.T) {
	t.Setenv("CLEANUP_INTERVAL", "0s")

	_, err := config.LoadMaster()
	if err == nil {
		t.Fatal("expected non-positive duration error")
	}
}

func TestLoadMasterRejectsInvalidWorkerStaleTimeout(t *testing.T) {
	t.Setenv("WORKER_STALE_TIMEOUT", "not-a-duration")

	_, err := config.LoadMaster()
	if err == nil {
		t.Fatal("expected invalid worker stale timeout error")
	}
}

func TestLoadMasterRejectsInvalidMasterShutdownTimeout(t *testing.T) {
	t.Setenv("MASTER_SHUTDOWN_TIMEOUT", "not-a-duration")

	_, err := config.LoadMaster()
	if err == nil {
		t.Fatal("expected invalid master shutdown timeout error")
	}
}

func TestLoadMasterReadsCertificateEnvironment(t *testing.T) {
	t.Setenv("CERT_DIR", "/etc/wasmcat/certs")
	t.Setenv("AUTO_GENERATE_CERTS", "false")
	t.Setenv("DEV_WORKER_ID", "worker-prod-01")
	t.Setenv("WORKER_STALE_TIMEOUT", "45s")
	t.Setenv("MASTER_SHUTDOWN_TIMEOUT", "8s")
	t.Setenv("MIN_WORKER_CPU_FREE", "15.5")
	t.Setenv("MIN_WORKER_RAM_FREE_MB", "512")
	t.Setenv("EXECUTE_CLIENT_ALLOWLIST", "wasmcat-client, deployer.internal ")
	t.Setenv("MAX_EXECUTION_REQUEST_BYTES", "4096")

	cfg, err := config.LoadMaster()
	if err != nil {
		t.Fatalf("LoadMaster returned error: %v", err)
	}

	if cfg.CertDir != "/etc/wasmcat/certs" {
		t.Fatalf("expected cert dir /etc/wasmcat/certs, got %q", cfg.CertDir)
	}
	if cfg.AutoGenerateCerts {
		t.Fatal("expected auto certificate generation to be disabled")
	}
	if cfg.WorkerIDForCert != "worker-prod-01" {
		t.Fatalf("expected worker-prod-01, got %q", cfg.WorkerIDForCert)
	}
	if cfg.WorkerStaleTimeout != 45*time.Second {
		t.Fatalf("expected worker stale timeout 45s, got %s", cfg.WorkerStaleTimeout)
	}
	if cfg.ShutdownTimeout != 8*time.Second {
		t.Fatalf("expected master shutdown timeout 8s, got %s", cfg.ShutdownTimeout)
	}
	if cfg.MinWorkerCPUFree != 15.5 {
		t.Fatalf("expected min CPU 15.5, got %f", cfg.MinWorkerCPUFree)
	}
	if cfg.MinWorkerRAMFreeMB != 512 {
		t.Fatalf("expected min RAM 512, got %f", cfg.MinWorkerRAMFreeMB)
	}
	if len(cfg.ExecuteClientIDs) != 2 || cfg.ExecuteClientIDs[0] != "wasmcat-client" || cfg.ExecuteClientIDs[1] != "deployer.internal" {
		t.Fatalf("unexpected execute client allowlist: %+v", cfg.ExecuteClientIDs)
	}
	if cfg.MaxExecuteBodyBytes != 4096 {
		t.Fatalf("expected max execute body bytes 4096, got %d", cfg.MaxExecuteBodyBytes)
	}
}

func TestLoadMasterRejectsInvalidAutoGenerateCerts(t *testing.T) {
	t.Setenv("AUTO_GENERATE_CERTS", "maybe")

	_, err := config.LoadMaster()
	if err == nil {
		t.Fatal("expected invalid boolean error")
	}
}

func TestLoadMasterRejectsInvalidCapacityThresholds(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		value string
	}{
		{name: "negative cpu", env: "MIN_WORKER_CPU_FREE", value: "-1"},
		{name: "cpu over one hundred", env: "MIN_WORKER_CPU_FREE", value: "101"},
		{name: "negative ram", env: "MIN_WORKER_RAM_FREE_MB", value: "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, tt.value)

			_, err := config.LoadMaster()
			if err == nil {
				t.Fatalf("expected invalid %s error", tt.env)
			}
		})
	}
}

func TestLoadMasterRejectsNonPositiveExecutionBodyLimit(t *testing.T) {
	t.Setenv("MAX_EXECUTION_REQUEST_BYTES", "0")

	_, err := config.LoadMaster()
	if err == nil {
		t.Fatal("expected non-positive max execution request bytes error")
	}
}

func TestLoadWorkerRejectsInvalidCoordinates(t *testing.T) {
	t.Setenv("WORKER_LATITUDE", "north")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("expected invalid latitude error")
	}
}

func TestLoadWorkerRejectsNonPositiveHeartbeatInterval(t *testing.T) {
	t.Setenv("HEARTBEAT_INTERVAL", "0s")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("expected non-positive heartbeat interval error")
	}
}

func TestLoadWorkerRejectsNonPositiveLimitDuration(t *testing.T) {
	t.Setenv("EXECUTION_TIMEOUT", "-1s")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("expected non-positive worker limit duration error")
	}
}

func TestLoadWorkerRejectsNonPositiveNumericLimits(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{name: "module bytes", env: "MAX_MODULE_BYTES"},
		{name: "payload bytes", env: "MAX_PAYLOAD_BYTES"},
		{name: "output bytes", env: "MAX_OUTPUT_BYTES"},
		{name: "concurrent execs", env: "MAX_CONCURRENT_EXECS"},
		{name: "cached modules", env: "MAX_CACHED_MODULES"},
		{name: "cache bytes", env: "MAX_CACHE_BYTES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.env, "0")

			_, err := config.LoadWorker()
			if err == nil {
				t.Fatalf("expected non-positive %s error", tt.env)
			}
		})
	}
}

func TestLoadWorkerDefaultsShutdownTimeoutFromExecutionTimeout(t *testing.T) {
	t.Setenv("EXECUTION_TIMEOUT", "20s")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker returned error: %v", err)
	}

	if cfg.Limits.ShutdownTimeout != 25*time.Second {
		t.Fatalf("expected shutdown timeout 25s, got %s", cfg.Limits.ShutdownTimeout)
	}
}
