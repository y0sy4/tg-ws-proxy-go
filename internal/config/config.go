// Package config provides configuration management.
package config

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config holds the proxy configuration.
type Config struct {
	Port      int      `json:"port"`
	Host      string   `json:"host"`
	DCIP      []string `json:"dc_ip"`
	Verbose   bool     `json:"verbose"`
	AutoStart bool     `json:"autostart"`
	LogMaxMB  float64  `json:"log_max_mb"`
	BufKB     int      `json:"buf_kb"`
	PoolSize  int      `json:"pool_size"`
	Auth      string   `json:"auth"` // username:password
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Port:      1080,
		Host:      "127.0.0.1",
		DCIP:      []string{"2:149.154.167.220", "4:149.154.167.220"},
		Verbose:   false,
		AutoStart: false,
		LogMaxMB:  5,
		BufKB:     256,
		PoolSize:  4,
	}
}

// GetConfigDir returns the configuration directory for the current OS.
func GetConfigDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			appData = home
		}
		return filepath.Join(appData, "TgWsProxy"), nil

	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "TgWsProxy"), nil

	default: // Linux and others
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig != "" {
			return filepath.Join(xdgConfig, "TgWsProxy"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "TgWsProxy"), nil
	}
}

// Load loads configuration from file.
func Load() (*Config, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return DefaultConfig(), nil
	}

	configPath := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), nil
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return DefaultConfig(), nil
	}

	// Ensure defaults for missing fields
	if cfg.Port == 0 {
		cfg.Port = 1080
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if len(cfg.DCIP) == 0 {
		cfg.DCIP = []string{"2:149.154.167.220", "4:149.154.167.220"}
	}

	return cfg, nil
}

// Save saves configuration to file.
func (c *Config) Save() error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// ParseDCIPList parses a list of "DC:IP" strings into a map.
func ParseDCIPList(dcIPList []string) (map[int]string, error) {
	result := make(map[int]string)
	for _, entry := range dcIPList {
		if !strings.Contains(entry, ":") {
			return nil, ErrInvalidDCIPFormat{Entry: entry}
		}
		parts := strings.SplitN(entry, ":", 2)
		dcStr, ipStr := parts[0], parts[1]

		dc, err := strconv.Atoi(dcStr)
		if err != nil {
			return nil, ErrInvalidDCIPFormat{Entry: entry}
		}

		if net.ParseIP(ipStr) == nil {
			return nil, ErrInvalidDCIPFormat{Entry: entry}
		}

		result[dc] = ipStr
	}
	return result, nil
}

// ErrInvalidDCIPFormat is returned when DC:IP format is invalid.
type ErrInvalidDCIPFormat struct {
	Entry string
}

func (e ErrInvalidDCIPFormat) Error() string {
	return "invalid --dc-ip format " + strconv.Quote(e.Entry) + ", expected DC:IP"
}
