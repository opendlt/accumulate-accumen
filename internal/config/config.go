package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the Accumen configuration
type Config struct {
	APIV3Endpoints []string `yaml:"apiV3Endpoints"`
	BlockTime      string   `yaml:"blockTime"`
	AnchorEvery    string   `yaml:"anchorEvery"`
	AnchorEveryN   int      `yaml:"anchorEveryBlocks"`
	GasScheduleID  string   `yaml:"gasScheduleID"`
	DNPaths        struct {
		Anchors string `yaml:"anchorsBase"`
		TxMeta  string `yaml:"txBase"`
	} `yaml:"dnPaths"`
	Storage struct {
		Backend string `yaml:"backend"` // "memory" | "badger"
		Path    string `yaml:"path"`    // e.g. "data/l1"
	} `yaml:"storage"`
	SequencerKey string `yaml:"sequencerKey"` // hex or base64, placeholder
}

// Load reads and parses a YAML configuration file
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Set defaults and validate
	if err := config.setDefaults(); err != nil {
		return nil, fmt.Errorf("failed to set config defaults: %w", err)
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// setDefaults sets default values for empty fields
func (c *Config) setDefaults() error {
	// Set default API endpoints if none provided
	if len(c.APIV3Endpoints) == 0 {
		c.APIV3Endpoints = []string{"https://mainnet.accumulatenetwork.io/v3"}
	}

	// Set default block time if empty
	if c.BlockTime == "" {
		c.BlockTime = "1s"
	}

	// Set default anchor interval if empty
	if c.AnchorEvery == "" {
		c.AnchorEvery = "10s"
	}

	// Set default anchor block count if not specified
	if c.AnchorEveryN == 0 {
		c.AnchorEveryN = 10
	}

	// Set default gas schedule ID if empty
	if c.GasScheduleID == "" {
		c.GasScheduleID = "default"
	}

	// Set default DN paths if empty
	if c.DNPaths.Anchors == "" {
		c.DNPaths.Anchors = "acc://dn.acme/anchors"
	}
	if c.DNPaths.TxMeta == "" {
		c.DNPaths.TxMeta = "acc://dn.acme/tx-metadata"
	}

	// Set default storage configuration
	if c.Storage.Backend == "" {
		c.Storage.Backend = "memory"
	}
	if c.Storage.Path == "" {
		c.Storage.Path = "data/l1"
	}

	return nil
}

// validate performs basic validation of config values
func (c *Config) validate() error {
	// Validate API endpoints
	if len(c.APIV3Endpoints) == 0 {
		return fmt.Errorf("at least one API v3 endpoint must be specified")
	}

	for i, endpoint := range c.APIV3Endpoints {
		if endpoint == "" {
			return fmt.Errorf("API v3 endpoint %d cannot be empty", i)
		}
	}

	// Validate block time duration
	if _, err := time.ParseDuration(c.BlockTime); err != nil {
		return fmt.Errorf("invalid block time duration %s: %w", c.BlockTime, err)
	}

	// Validate anchor interval duration
	if _, err := time.ParseDuration(c.AnchorEvery); err != nil {
		return fmt.Errorf("invalid anchor interval duration %s: %w", c.AnchorEvery, err)
	}

	// Validate anchor block count
	if c.AnchorEveryN < 1 {
		return fmt.Errorf("anchor every N blocks must be at least 1, got %d", c.AnchorEveryN)
	}

	// Validate DN paths are not empty
	if c.DNPaths.Anchors == "" {
		return fmt.Errorf("DN anchors path cannot be empty")
	}
	if c.DNPaths.TxMeta == "" {
		return fmt.Errorf("DN tx metadata path cannot be empty")
	}

	// Validate storage backend
	if c.Storage.Backend != "memory" && c.Storage.Backend != "badger" {
		return fmt.Errorf("storage backend must be 'memory' or 'badger', got %s", c.Storage.Backend)
	}
	if c.Storage.Path == "" {
		return fmt.Errorf("storage path cannot be empty")
	}

	return nil
}

// GetBlockTimeDuration returns the block time as a time.Duration
func (c *Config) GetBlockTimeDuration() time.Duration {
	duration, err := time.ParseDuration(c.BlockTime)
	if err != nil {
		// This should not happen if validation passed
		return time.Second
	}
	return duration
}

// GetAnchorIntervalDuration returns the anchor interval as a time.Duration
func (c *Config) GetAnchorIntervalDuration() time.Duration {
	duration, err := time.ParseDuration(c.AnchorEvery)
	if err != nil {
		// This should not happen if validation passed
		return 10 * time.Second
	}
	return duration
}

// String returns a string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Endpoints: %v, BlockTime: %s, AnchorEvery: %s, AnchorEveryN: %d}",
		c.APIV3Endpoints, c.BlockTime, c.AnchorEvery, c.AnchorEveryN)
}