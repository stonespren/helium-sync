// Package config manages helium-sync configuration, device identity, and paths.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

const (
	DefaultHeliumDir   = ".config/net.imput.helium"
	ConfigDirName      = "helium-sync"
	StateDirName       = "helium-sync"
	ConfigFileName     = "config.json"
	DeviceIDFileName   = "device_id"
	LockFileName       = "helium-sync.lock"
	DefaultSyncInterval = 15 // minutes
)

// Config represents the application configuration.
type Config struct {
	HeliumDir    string   `json:"helium_dir"`
	S3Bucket     string   `json:"s3_bucket"`
	S3Region     string   `json:"s3_region"`
	AWSProfile   string   `json:"aws_profile,omitempty"`
	SyncInterval int      `json:"sync_interval_minutes"`
	SyncProfiles []string `json:"sync_profiles,omitempty"`
	LogLevel     string   `json:"log_level"`
	SSE          bool     `json:"sse_s3"`
}

var (
	configOnce sync.Once
	cached     *Config
	cachedErr  error
)

// ConfigDir returns the config directory path.
func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", ConfigDirName)
}

// StateDir returns the state/log directory path.
func StateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", StateDirName)
}

// ConfigFilePath returns the full path to config.json.
func ConfigFilePath() string {
	return filepath.Join(ConfigDir(), ConfigFileName)
}

// DeviceIDPath returns the path to the device_id file.
func DeviceIDPath() string {
	return filepath.Join(ConfigDir(), DeviceIDFileName)
}

// LockFilePath returns the path to the lock file.
func LockFilePath() string {
	return filepath.Join(ConfigDir(), LockFileName)
}

// DefaultHeliumPath returns the default Helium browser config path.
func DefaultHeliumPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, DefaultHeliumDir)
}

// Load reads the configuration from disk.
func Load() (*Config, error) {
	configOnce.Do(func() {
		cached, cachedErr = loadFromDisk()
	})
	return cached, cachedErr
}

// ForceLoad reads the configuration without caching.
func ForceLoad() (*Config, error) {
	return loadFromDisk()
}

func loadFromDisk() (*Config, error) {
	data, err := os.ReadFile(ConfigFilePath())
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = DefaultSyncInterval
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	return &cfg, nil
}

// Save writes the configuration to disk.
func Save(cfg *Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(ConfigFilePath(), data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	// Reset cache
	configOnce = sync.Once{}
	cached = nil
	cachedErr = nil
	return nil
}

// GetDeviceID returns the persistent device ID, creating one if needed.
func GetDeviceID() (string, error) {
	path := DeviceIDPath()
	data, err := os.ReadFile(path)
	if err == nil {
		id := string(data)
		if len(id) > 0 {
			return id, nil
		}
	}
	// Generate new device ID
	id := uuid.New().String()
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		return "", fmt.Errorf("writing device ID: %w", err)
	}
	return id, nil
}

// Exists returns true if a config file exists.
func Exists() bool {
	_, err := os.Stat(ConfigFilePath())
	return err == nil
}
