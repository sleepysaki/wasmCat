package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wasmcat/internal/bootstrap"
	"wasmcat/internal/security"
)

func TestInitMasterWritesProductionEnv(t *testing.T) {
	configDir := t.TempDir()
	certDir := filepath.Join(configDir, "certs")

	result, err := bootstrap.InitMaster(bootstrap.MasterOptions{
		ConfigDir:           configDir,
		CertDir:             certDir,
		Port:                "9443",
		CleanupInterval:     "30s",
		WorkerStaleTimeout:  "90s",
		ShutdownTimeout:     "12s",
		MinWorkerCPUFree:    10,
		MinWorkerRAMFreeMB:  256,
		ExecuteClientIDs:    "wasmcat-client,deployer.internal",
		MaxExecuteBodyBytes: 4096,
		DevWorkerID:         "worker-test-01",
	})
	if err != nil {
		t.Fatalf("InitMaster returned error: %v", err)
	}

	if result.ConfigPath != filepath.Join(configDir, "master.env") {
		t.Fatalf("unexpected config path %q", result.ConfigPath)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected production cert warning")
	}

	env := readFile(t, result.ConfigPath)
	assertContains(t, env, "MASTER_PORT=9443\n")
	assertContains(t, env, "CERT_DIR="+certDir+"\n")
	assertContains(t, env, "AUTO_GENERATE_CERTS=false\n")
	assertContains(t, env, "DEV_WORKER_ID=worker-test-01\n")
	assertContains(t, env, "CLEANUP_INTERVAL=30s\n")
	assertContains(t, env, "WORKER_STALE_TIMEOUT=90s\n")
	assertContains(t, env, "MASTER_SHUTDOWN_TIMEOUT=12s\n")
	assertContains(t, env, "MIN_WORKER_CPU_FREE=10\n")
	assertContains(t, env, "MIN_WORKER_RAM_FREE_MB=256\n")
	assertContains(t, env, "EXECUTE_CLIENT_ALLOWLIST=wasmcat-client,deployer.internal\n")
	assertContains(t, env, "MAX_EXECUTION_REQUEST_BYTES=4096\n")
}

func TestInitMasterRefusesToOverwriteEnvWithoutForce(t *testing.T) {
	configDir := t.TempDir()
	certDir := filepath.Join(configDir, "certs")

	_, err := bootstrap.InitMaster(bootstrap.MasterOptions{
		ConfigDir: configDir,
		CertDir:   certDir,
	})
	if err != nil {
		t.Fatalf("first InitMaster returned error: %v", err)
	}

	_, err = bootstrap.InitMaster(bootstrap.MasterOptions{
		ConfigDir: configDir,
		CertDir:   certDir,
	})
	if err == nil {
		t.Fatal("expected overwrite protection error")
	}
}

func TestInitMasterRejectsNonPositiveDurations(t *testing.T) {
	_, err := bootstrap.InitMaster(bootstrap.MasterOptions{
		ConfigDir:       "config",
		CertDir:         "certs",
		CleanupInterval: "0s",
	})
	if err == nil {
		t.Fatal("expected non-positive cleanup interval error")
	}
}

func TestInitMasterRejectsInvalidCapacityThresholds(t *testing.T) {
	tests := []struct {
		name    string
		options bootstrap.MasterOptions
	}{
		{
			name: "negative cpu",
			options: bootstrap.MasterOptions{
				MinWorkerCPUFree: -1,
			},
		},
		{
			name: "cpu over one hundred",
			options: bootstrap.MasterOptions{
				MinWorkerCPUFree: 101,
			},
		},
		{
			name: "negative ram",
			options: bootstrap.MasterOptions{
				MinWorkerRAMFreeMB: -1,
			},
		},
		{
			name: "negative request body limit",
			options: bootstrap.MasterOptions{
				MaxExecuteBodyBytes: -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.options.ConfigDir = "config"
			tt.options.CertDir = "certs"

			_, err := bootstrap.InitMaster(tt.options)
			if err == nil {
				t.Fatal("expected invalid master capacity/body limit error")
			}
		})
	}
}

func TestInitMasterGeneratesDevelopmentCerts(t *testing.T) {
	configDir := t.TempDir()
	certDir := filepath.Join(configDir, "certs")
	workerID := "worker-dev-01"

	_, err := bootstrap.InitMaster(bootstrap.MasterOptions{
		ConfigDir:        configDir,
		CertDir:          certDir,
		DevWorkerID:      workerID,
		GenerateDevCerts: true,
	})
	if err != nil {
		t.Fatalf("InitMaster returned error: %v", err)
	}

	assertFileExists(t, security.CACertPath(certDir))
	assertFileExists(t, security.MasterCertPath(certDir))
	assertFileExists(t, security.MasterKeyPath(certDir))
	assertFileExists(t, security.WorkerCertPath(certDir, workerID))
	assertFileExists(t, security.WorkerKeyPath(certDir, workerID))
}

func TestInitWorkerWritesEnv(t *testing.T) {
	configDir := t.TempDir()
	certDir := filepath.Join(configDir, "certs")

	result, err := bootstrap.InitWorker(bootstrap.WorkerOptions{
		ConfigDir:          configDir,
		CertDir:            certDir,
		WorkerID:           "worker-us-01",
		Port:               "9444",
		MasterURL:          "https://master.example.com:7270",
		AdvertiseAddress:   "worker-us-01.example.com:9444",
		Latitude:           40.7128,
		Longitude:          -74.006,
		ShutdownTimeout:    "9s",
		MaxModuleBytes:     10 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 8,
		MaxCachedModules:   16,
		MaxCacheBytes:      64 << 20,
		ModuleCacheTTL:     "10m",
	})
	if err != nil {
		t.Fatalf("InitWorker returned error: %v", err)
	}

	env := readFile(t, result.ConfigPath)
	assertContains(t, env, "WORKER_ID=worker-us-01\n")
	assertContains(t, env, "WORKER_PORT=9444\n")
	assertContains(t, env, "MASTER_URL=https://master.example.com:7270\n")
	assertContains(t, env, "WORKER_ADVERTISE_ADDRESS=worker-us-01.example.com:9444\n")
	assertContains(t, env, "WORKER_LATITUDE=40.7128\n")
	assertContains(t, env, "WORKER_LONGITUDE=-74.006\n")
	assertContains(t, env, "WORKER_SHUTDOWN_TIMEOUT=9s\n")
	assertContains(t, env, "MAX_CONCURRENT_EXECS=8\n")
	assertContains(t, env, "MAX_CACHED_MODULES=16\n")
	assertContains(t, env, "MAX_CACHE_BYTES=67108864\n")
	assertContains(t, env, "MODULE_CACHE_TTL=10m\n")
}

func TestInitWorkerRejectsInvalidMasterURL(t *testing.T) {
	_, err := bootstrap.InitWorker(bootstrap.WorkerOptions{
		ConfigDir: "config",
		CertDir:   "certs",
		MasterURL: "http://master.example.com:7270",
	})
	if err == nil {
		t.Fatal("expected invalid master URL error")
	}
}

func TestInitWorkerRejectsNonPositiveDurations(t *testing.T) {
	_, err := bootstrap.InitWorker(bootstrap.WorkerOptions{
		ConfigDir:          "config",
		CertDir:            "certs",
		WorkerID:           "worker-test",
		MasterURL:          "https://master.example.com:7270",
		AdvertiseAddress:   "worker.example.com:7271",
		HeartbeatInterval:  "0s",
		MaxModuleBytes:     10 << 20,
		MaxPayloadBytes:    1 << 20,
		MaxOutputBytes:     1 << 20,
		MaxConcurrentExecs: 1,
		MaxCachedModules:   1,
		MaxCacheBytes:      1 << 20,
	})
	if err == nil {
		t.Fatal("expected non-positive heartbeat interval error")
	}
}

func TestInitWorkerRejectsInvalidLimits(t *testing.T) {
	_, err := bootstrap.InitWorker(bootstrap.WorkerOptions{
		ConfigDir:        "config",
		CertDir:          "certs",
		MasterURL:        "https://master.example.com:7270",
		AdvertiseAddress: "worker.example.com:7271",
	})
	if err == nil {
		t.Fatal("expected invalid worker limit error")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(content)
}

func assertContains(t *testing.T, content string, expected string) {
	t.Helper()

	if !strings.Contains(content, expected) {
		t.Fatalf("expected %q to contain %q", content, expected)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
