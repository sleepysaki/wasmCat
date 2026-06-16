package ctl_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"wasmcat/internal/ctl"
)

func TestSaveAndLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := ctl.Config{
		MasterURL:      "https://localhost:7270/",
		CACert:         "./certs/ca.crt",
		ClientCert:     "./certs/client.crt",
		ClientKey:      "./certs/client.key",
		DefaultUserLat: 21.0278,
		DefaultUserLon: 105.8342,
		Output:         "json",
	}

	if err := ctl.SaveConfig(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := ctl.LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.MasterURL != "https://localhost:7270" {
		t.Fatalf("expected normalized master url, got %q", loaded.MasterURL)
	}
	if loaded.Output != "json" {
		t.Fatalf("expected json output, got %q", loaded.Output)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("expected config permissions 0600, got %v", info.Mode().Perm())
	}
}

func TestConfigValidationRequiresTLSFiles(t *testing.T) {
	cfg := ctl.Config{MasterURL: "https://localhost:7270"}

	if err := cfg.ValidateForRequest(); err == nil {
		t.Fatalf("expected validation error")
	}
}
