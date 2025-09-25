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
	Pricing struct {
		GasScheduleID   string `yaml:"gasScheduleID"`   // Gas schedule ID to fetch from DN registry
		RefreshInterval string `yaml:"refreshInterval"` // How often to refresh schedule (e.g., "60s")
	} `yaml:"pricing"`
	Submitter struct {
		BatchSize   int    `yaml:"batchSize"`   // Number of items to process per batch
		BackoffMin  string `yaml:"backoffMin"`  // Minimum backoff duration (e.g., "1s")
		BackoffMax  string `yaml:"backoffMax"`  // Maximum backoff duration (e.g., "5m")
	} `yaml:"submitter"`
	Confirm struct {
		WaitForExecution bool   `yaml:"waitForExecution"` // Wait for transaction execution confirmation
		Timeout          string `yaml:"timeout"`          // Timeout for execution confirmation (e.g., "30s")
	} `yaml:"confirm"`
	Snapshots struct {
		EveryN int `yaml:"everyN"` // Take snapshot every N blocks (0 = disabled)
		Retain int `yaml:"retain"` // Number of snapshots to retain (0 = keep all)
	} `yaml:"snapshots"`
	Signer struct {
		Type string `yaml:"type"` // "file" | "env" | "dev"
		Key  string `yaml:"key"`  // path or env name or raw key (dev only)
	} `yaml:"signer"`
	SequencerKey string `yaml:"sequencerKey"` // DEPRECATED: hex or base64, use Signer instead
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

	// Set default pricing configuration
	if c.Pricing.GasScheduleID == "" {
		c.Pricing.GasScheduleID = "default"
	}
	if c.Pricing.RefreshInterval == "" {
		c.Pricing.RefreshInterval = "60s"
	}

	// Set default submitter configuration
	if c.Submitter.BatchSize == 0 {
		c.Submitter.BatchSize = 10
	}
	if c.Submitter.BackoffMin == "" {
		c.Submitter.BackoffMin = "1s"
	}
	if c.Submitter.BackoffMax == "" {
		c.Submitter.BackoffMax = "5m"
	}

	// Set default confirmation configuration
	if c.Confirm.Timeout == "" {
		c.Confirm.Timeout = "30s"
	}

	// Set default snapshot configuration
	if c.Snapshots.EveryN == 0 {
		c.Snapshots.EveryN = 100 // Snapshot every 100 blocks by default
	}
	if c.Snapshots.Retain == 0 {
		c.Snapshots.Retain = 5 // Keep 5 snapshots by default
	}

	// Set default signer configuration
	if c.Signer.Type == "" {
		// If legacy SequencerKey is provided, use dev signer
		if c.SequencerKey != "" {
			c.Signer.Type = "dev"
			c.Signer.Key = c.SequencerKey
		} else {
			// Default to dev signer with generated key
			c.Signer.Type = "dev"
		}
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

	// Validate pricing configuration
	if c.Pricing.GasScheduleID == "" {
		return fmt.Errorf("gas schedule ID cannot be empty")
	}
	if c.Pricing.RefreshInterval != "" {
		if _, err := time.ParseDuration(c.Pricing.RefreshInterval); err != nil {
			return fmt.Errorf("invalid pricing refresh interval %s: %w", c.Pricing.RefreshInterval, err)
		}
	}

	// Validate submitter configuration
	if c.Submitter.BatchSize < 1 {
		return fmt.Errorf("submitter batch size must be at least 1, got %d", c.Submitter.BatchSize)
	}
	if c.Submitter.BatchSize > 1000 {
		return fmt.Errorf("submitter batch size must be at most 1000, got %d", c.Submitter.BatchSize)
	}
	if _, err := time.ParseDuration(c.Submitter.BackoffMin); err != nil {
		return fmt.Errorf("invalid submitter backoff min %s: %w", c.Submitter.BackoffMin, err)
	}
	if _, err := time.ParseDuration(c.Submitter.BackoffMax); err != nil {
		return fmt.Errorf("invalid submitter backoff max %s: %w", c.Submitter.BackoffMax, err)
	}

	// Validate backoff min <= backoff max
	minDuration, _ := time.ParseDuration(c.Submitter.BackoffMin)
	maxDuration, _ := time.ParseDuration(c.Submitter.BackoffMax)
	if minDuration > maxDuration {
		return fmt.Errorf("submitter backoff min (%s) cannot be greater than backoff max (%s)", c.Submitter.BackoffMin, c.Submitter.BackoffMax)
	}

	// Validate confirmation configuration
	if _, err := time.ParseDuration(c.Confirm.Timeout); err != nil {
		return fmt.Errorf("invalid confirmation timeout %s: %w", c.Confirm.Timeout, err)
	}

	// Validate snapshot configuration
	if c.Snapshots.EveryN < 0 {
		return fmt.Errorf("snapshots everyN must be non-negative, got %d", c.Snapshots.EveryN)
	}
	if c.Snapshots.Retain < 0 {
		return fmt.Errorf("snapshots retain must be non-negative, got %d", c.Snapshots.Retain)
	}

	// Validate signer configuration
	if c.Signer.Type != "" {
		switch c.Signer.Type {
		case "file":
			if c.Signer.Key == "" {
				return fmt.Errorf("file signer requires key path")
			}
		case "env":
			if c.Signer.Key == "" {
				return fmt.Errorf("env signer requires environment variable name")
			}
		case "dev":
			// Key is optional for dev signer (will generate if empty)
		default:
			return fmt.Errorf("unsupported signer type: %s (must be 'file', 'env', or 'dev')", c.Signer.Type)
		}
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

// GetPricingRefreshDuration returns the pricing refresh interval as a time.Duration
func (c *Config) GetPricingRefreshDuration() time.Duration {
	duration, err := time.ParseDuration(c.Pricing.RefreshInterval)
	if err != nil {
		// This should not happen if validation passed
		return 60 * time.Second
	}
	return duration
}

// GetBackoffMinDuration returns the minimum backoff duration as a time.Duration
func (c *Config) GetBackoffMinDuration() time.Duration {
	duration, err := time.ParseDuration(c.Submitter.BackoffMin)
	if err != nil {
		// This should not happen if validation passed
		return 1 * time.Second
	}
	return duration
}

// GetBackoffMaxDuration returns the maximum backoff duration as a time.Duration
func (c *Config) GetBackoffMaxDuration() time.Duration {
	duration, err := time.ParseDuration(c.Submitter.BackoffMax)
	if err != nil {
		// This should not happen if validation passed
		return 5 * time.Minute
	}
	return duration
}

// GetConfirmTimeoutDuration returns the confirmation timeout as a time.Duration
func (c *Config) GetConfirmTimeoutDuration() time.Duration {
	duration, err := time.ParseDuration(c.Confirm.Timeout)
	if err != nil {
		// This should not happen if validation passed
		return 30 * time.Second
	}
	return duration
}

// String returns a string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Endpoints: %v, BlockTime: %s, AnchorEvery: %s, AnchorEveryN: %d, GasScheduleID: %s, BatchSize: %d}",
		c.APIV3Endpoints, c.BlockTime, c.AnchorEvery, c.AnchorEveryN, c.Pricing.GasScheduleID, c.Submitter.BatchSize)
}