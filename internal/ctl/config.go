package ctl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const ConfigDirName = ".wasmcat"

type Config struct {
	MasterURL      string  `json:"master_url"`
	CACert         string  `json:"ca_cert"`
	ClientCert     string  `json:"client_cert"`
	ClientKey      string  `json:"client_key"`
	DefaultUserLat float64 `json:"default_user_lat"`
	DefaultUserLon float64 `json:"default_user_lon"`
	Output         string  `json:"output,omitempty"`
}

func DefaultConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}

	return filepath.Join(homeDir, ConfigDirName, "config.json"), nil
}

func LoadConfig(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return Config{}, err
		}
		path = defaultPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config file %s does not exist; run wasmcatctl config init first", path)
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config file: %w", err)
	}
	cfg.normalize()
	return cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return err
		}
		path = defaultPath
	}
	cfg.normalize()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

func DefaultLocalConfig() Config {
	separator := string(os.PathSeparator)
	certDir := "." + separator + "local" + separator + "certs"
	return Config{
		MasterURL:      "https://localhost:7270",
		CACert:         filepath.Join(certDir, "ca.crt"),
		ClientCert:     filepath.Join(certDir, "worker-worker-vn-01.crt"),
		ClientKey:      filepath.Join(certDir, "worker-worker-vn-01.key"),
		DefaultUserLat: 21.0278,
		DefaultUserLon: 105.8342,
		Output:         "table",
	}
}

func ApplyOverrides(cfg Config, masterURL string, caCert string, clientCert string, clientKey string, output string, userLat float64, userLon float64, hasUserLat bool, hasUserLon bool) Config {
	if strings.TrimSpace(masterURL) != "" {
		cfg.MasterURL = masterURL
	}
	if strings.TrimSpace(caCert) != "" {
		cfg.CACert = caCert
	}
	if strings.TrimSpace(clientCert) != "" {
		cfg.ClientCert = clientCert
	}
	if strings.TrimSpace(clientKey) != "" {
		cfg.ClientKey = clientKey
	}
	if strings.TrimSpace(output) != "" {
		cfg.Output = output
	}
	if hasUserLat {
		cfg.DefaultUserLat = userLat
	}
	if hasUserLon {
		cfg.DefaultUserLon = userLon
	}
	cfg.normalize()
	return cfg
}

func (cfg *Config) ValidateForRequest() error {
	cfg.normalize()
	if cfg.MasterURL == "" {
		return fmt.Errorf("master_url is required")
	}
	if cfg.CACert == "" {
		return fmt.Errorf("ca_cert is required")
	}
	if cfg.ClientCert == "" {
		return fmt.Errorf("client_cert is required")
	}
	if cfg.ClientKey == "" {
		return fmt.Errorf("client_key is required")
	}
	if cfg.DefaultUserLat < -90 || cfg.DefaultUserLat > 90 {
		return fmt.Errorf("default_user_lat must be between -90 and 90")
	}
	if cfg.DefaultUserLon < -180 || cfg.DefaultUserLon > 180 {
		return fmt.Errorf("default_user_lon must be between -180 and 180")
	}

	return nil
}

func (cfg *Config) normalize() {
	cfg.MasterURL = strings.TrimRight(strings.TrimSpace(cfg.MasterURL), "/")
	cfg.CACert = cleanPath(cfg.CACert)
	cfg.ClientCert = cleanPath(cfg.ClientCert)
	cfg.ClientKey = cleanPath(cfg.ClientKey)
	cfg.Output = strings.ToLower(strings.TrimSpace(cfg.Output))
	if cfg.Output == "" {
		cfg.Output = "table"
	}
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		return filepath.Clean(path)
	}

	return filepath.Clean(path)
}
