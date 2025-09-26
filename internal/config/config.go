package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the Accumen configuration
type Config struct {
	APIV3Endpoints []string `yaml:"apiV3Endpoints"` // DEPRECATED: use L0.Static instead
	BlockTime      string   `yaml:"blockTime"`
	AnchorEvery    string   `yaml:"anchorEvery"`
	AnchorEveryN   int      `yaml:"anchorEveryBlocks"`
	GasScheduleID  string   `yaml:"gasScheduleID"`
	L0 struct {
		Source string   `yaml:"source"`          // "proxy" | "static"
		Proxy  string   `yaml:"proxy"`           // http(s)://...
		Static []string `yaml:"apiV3Endpoints"`  // static endpoint list
		WSPath string   `yaml:"wsPath"`          // WebSocket path, default "/ws"
	} `yaml:"l0"`
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
	Keystore struct {
		Path string `yaml:"path"` // path to keystore directory
	} `yaml:"keystore"`
	Namespace struct {
		ReservedLabel string `yaml:"reservedLabel"` // default "accumen"
		Enforce       bool   `yaml:"enforce"`       // enforce namespace restrictions
	} `yaml:"namespace"`
	Events struct {
		ReconnectMin string `yaml:"reconnectMin"` // minimum reconnect delay (e.g., "1s")
		ReconnectMax string `yaml:"reconnectMax"` // maximum reconnect delay (e.g., "60s")
		Enable       bool   `yaml:"enable"`       // enable event monitoring for confirmations
	} `yaml:"events"`
	Credits struct {
		MinBuffer    uint64 `yaml:"minBuffer"`    // credits to maintain
		Target       uint64 `yaml:"target"`       // refill to this amount
		FundingToken string `yaml:"fundingToken"` // acc://.../tokens used to pay ACME for credits
	} `yaml:"credits"`
	Network struct {
		Name string `yaml:"name"` // devnet|testnet|mainnet
	} `yaml:"network"`
	Server struct {
		Addr string `yaml:"addr"`
		TLS  struct {
			Cert string `yaml:"cert"`
			Key  string `yaml:"key"`
		} `yaml:"tls"`
		APIKeys []string `yaml:"apiKeys"`
		CORS    struct {
			Origins []string `yaml:"origins"`
		} `yaml:"cors"`
		Rate struct {
			RPS   int `yaml:"rps"`
			Burst int `yaml:"burst"`
		} `yaml:"rate"`
	} `yaml:"server"`
	Profiles struct {
		Name string `yaml:"name"` // "mainnet"|"testnet"|"devnet"|"local"
		Path string `yaml:"path"` // optional explicit file path
	} `yaml:"profiles"`
	SequencerKey string `yaml:"sequencerKey"`       // DEPRECATED: hex or base64, use Signer instead
	networkName  string // internal field to track the effective network
}

// Load reads and parses a YAML configuration file
func Load(path string) (*Config, error) {
	return LoadWithNetwork(path, "")
}

// LoadWithNetwork reads and parses a YAML configuration file with network profile support
func LoadWithNetwork(path, networkOverride string) (*Config, error) {
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

	// Determine effective network
	effectiveNetwork := determineNetwork(networkOverride, &config)
	config.networkName = effectiveNetwork

	// Load and merge network profile if not "local"
	if effectiveNetwork != "local" {
		if err := config.loadNetworkProfile(effectiveNetwork, path); err != nil {
			return nil, fmt.Errorf("failed to load network profile: %w", err)
		}
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

// determineNetwork determines the effective network name using priority:
// 1. networkOverride (CLI flag)
// 2. ACCUMEN_NETWORK environment variable
// 3. config.Profiles.Name
// 4. default "local"
func determineNetwork(networkOverride string, config *Config) string {
	if networkOverride != "" {
		return networkOverride
	}

	if envNetwork := os.Getenv("ACCUMEN_NETWORK"); envNetwork != "" {
		return envNetwork
	}

	if config.Network.Name != "" {
		return config.Network.Name
	}

	if config.Profiles.Name != "" {
		return config.Profiles.Name
	}

	return "local"
}

// loadNetworkProfile loads and merges a network profile into the base configuration
func (c *Config) loadNetworkProfile(networkName, baseConfigPath string) error {
	var profilePath string

	// Use explicit profile path if provided
	if c.Profiles.Path != "" {
		profilePath = c.Profiles.Path
	} else {
		// Determine profile path relative to config directory
		configDir := filepath.Dir(baseConfigPath)
		profilePath = filepath.Join(configDir, "networks", networkName+".yaml")
	}

	// Check if profile file exists
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		return fmt.Errorf("network profile file not found: %s", profilePath)
	}

	// Load profile configuration
	profileData, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("failed to read network profile %s: %w", profilePath, err)
	}

	var profileConfig Config
	if err := yaml.Unmarshal(profileData, &profileConfig); err != nil {
		return fmt.Errorf("failed to parse network profile %s: %w", profilePath, err)
	}

	// Merge profile into base config (base config wins on conflicts if explicitly set)
	c.mergeProfile(&profileConfig)

	return nil
}

// mergeProfile merges profile settings into the base config
// Base config values take precedence if they are explicitly set (non-zero/non-empty)
func (c *Config) mergeProfile(profile *Config) {
	// Merge L0 configuration
	if c.L0.Source == "" && profile.L0.Source != "" {
		c.L0.Source = profile.L0.Source
	}
	if c.L0.Proxy == "" && profile.L0.Proxy != "" {
		c.L0.Proxy = profile.L0.Proxy
	}
	if len(c.L0.Static) == 0 && len(profile.L0.Static) > 0 {
		c.L0.Static = profile.L0.Static
	}
	if c.L0.WSPath == "" && profile.L0.WSPath != "" {
		c.L0.WSPath = profile.L0.WSPath
	}

	// Merge legacy API endpoints
	if len(c.APIV3Endpoints) == 0 && len(profile.APIV3Endpoints) > 0 {
		c.APIV3Endpoints = profile.APIV3Endpoints
	}

	// Merge timing configurations
	if c.BlockTime == "" && profile.BlockTime != "" {
		c.BlockTime = profile.BlockTime
	}
	if c.AnchorEvery == "" && profile.AnchorEvery != "" {
		c.AnchorEvery = profile.AnchorEvery
	}
	if c.AnchorEveryN == 0 && profile.AnchorEveryN != 0 {
		c.AnchorEveryN = profile.AnchorEveryN
	}

	// Merge DN paths
	if c.DNPaths.Anchors == "" && profile.DNPaths.Anchors != "" {
		c.DNPaths.Anchors = profile.DNPaths.Anchors
	}
	if c.DNPaths.TxMeta == "" && profile.DNPaths.TxMeta != "" {
		c.DNPaths.TxMeta = profile.DNPaths.TxMeta
	}

	// Merge confirmation settings
	if !c.Confirm.WaitForExecution && profile.Confirm.WaitForExecution {
		c.Confirm.WaitForExecution = profile.Confirm.WaitForExecution
	}
	if c.Confirm.Timeout == "" && profile.Confirm.Timeout != "" {
		c.Confirm.Timeout = profile.Confirm.Timeout
	}

	// Merge events settings
	if c.Events.ReconnectMin == "" && profile.Events.ReconnectMin != "" {
		c.Events.ReconnectMin = profile.Events.ReconnectMin
	}
	if c.Events.ReconnectMax == "" && profile.Events.ReconnectMax != "" {
		c.Events.ReconnectMax = profile.Events.ReconnectMax
	}
	if !c.Events.Enable && profile.Events.Enable {
		c.Events.Enable = profile.Events.Enable
	}

	// Merge credits settings
	if c.Credits.MinBuffer == 0 && profile.Credits.MinBuffer != 0 {
		c.Credits.MinBuffer = profile.Credits.MinBuffer
	}
	if c.Credits.Target == 0 && profile.Credits.Target != 0 {
		c.Credits.Target = profile.Credits.Target
	}
	if c.Credits.FundingToken == "" && profile.Credits.FundingToken != "" {
		c.Credits.FundingToken = profile.Credits.FundingToken
	}

	// Merge keystore settings
	if c.Keystore.Path == "" && profile.Keystore.Path != "" {
		c.Keystore.Path = profile.Keystore.Path
	}
}

// EffectiveNetwork returns the name of the effective network profile being used
func (c *Config) EffectiveNetwork() string {
	return c.networkName
}

// setDefaults sets default values for empty fields
func (c *Config) setDefaults() error {
	// Set default L0 configuration
	if c.L0.Source == "" {
		// Default to static if not specified
		c.L0.Source = "static"
	}
	if c.L0.WSPath == "" {
		c.L0.WSPath = "/ws"
	}

	// Handle legacy APIV3Endpoints field
	if c.L0.Source == "static" && len(c.L0.Static) == 0 {
		if len(c.APIV3Endpoints) > 0 {
			// Migrate from legacy field
			c.L0.Static = c.APIV3Endpoints
		} else {
			// Set default endpoints
			c.L0.Static = []string{"https://mainnet.accumulatenetwork.io/v3"}
		}
	}

	// Set default API endpoints for backward compatibility
	if len(c.APIV3Endpoints) == 0 {
		if len(c.L0.Static) > 0 {
			c.APIV3Endpoints = c.L0.Static
		} else {
			c.APIV3Endpoints = []string{"https://mainnet.accumulatenetwork.io/v3"}
		}
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

	// Set default namespace configuration
	if c.Namespace.ReservedLabel == "" {
		c.Namespace.ReservedLabel = "accumen"
	}

	// Set default events configuration
	if c.Events.ReconnectMin == "" {
		c.Events.ReconnectMin = "1s"
	}
	if c.Events.ReconnectMax == "" {
		c.Events.ReconnectMax = "60s"
	}
	// Events.Enable defaults to false (no change needed)

	// Set default credits configuration
	if c.Credits.MinBuffer == 0 {
		c.Credits.MinBuffer = 1000 // Maintain at least 1000 credits
	}
	if c.Credits.Target == 0 {
		c.Credits.Target = 5000 // Refill to 5000 credits
	}
	if c.Credits.FundingToken == "" {
		c.Credits.FundingToken = "acc://acme.acme/tokens" // Default ACME token URL
	}

	// Set default keystore configuration
	if c.Keystore.Path == "" {
		c.Keystore.Path = "keystore" // Default keystore directory
	}

	// Set default server configuration
	if c.Server.Addr == "" {
		c.Server.Addr = ":8666" // Default RPC server address
	}
	if c.Server.Rate.RPS == 0 {
		c.Server.Rate.RPS = 100 // Default 100 requests per second
	}
	if c.Server.Rate.Burst == 0 {
		c.Server.Rate.Burst = 200 // Default burst of 200 requests
	}

	return nil
}

// validate performs basic validation of config values
func (c *Config) validate() error {
	// Validate L0 configuration
	switch c.L0.Source {
	case "proxy":
		if c.L0.Proxy == "" {
			return fmt.Errorf("l0.proxy must be specified when source is 'proxy'")
		}
	case "static":
		if len(c.L0.Static) == 0 {
			return fmt.Errorf("l0.static must contain at least one endpoint when source is 'static'")
		}
		for i, endpoint := range c.L0.Static {
			if endpoint == "" {
				return fmt.Errorf("l0.static endpoint %d cannot be empty", i)
			}
		}
	default:
		return fmt.Errorf("l0.source must be either 'proxy' or 'static', got: %s", c.L0.Source)
	}

	// Validate WebSocket path
	if c.L0.WSPath == "" {
		return fmt.Errorf("l0.wsPath cannot be empty")
	}

	// Validate legacy API endpoints for backward compatibility
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

	// Validate events configuration
	if c.Events.ReconnectMin != "" {
		if _, err := time.ParseDuration(c.Events.ReconnectMin); err != nil {
			return fmt.Errorf("invalid events reconnect min duration %s: %w", c.Events.ReconnectMin, err)
		}
	}
	if c.Events.ReconnectMax != "" {
		if _, err := time.ParseDuration(c.Events.ReconnectMax); err != nil {
			return fmt.Errorf("invalid events reconnect max duration %s: %w", c.Events.ReconnectMax, err)
		}
	}

	// Validate reconnect min <= reconnect max
	if c.Events.ReconnectMin != "" && c.Events.ReconnectMax != "" {
		minDuration, _ := time.ParseDuration(c.Events.ReconnectMin)
		maxDuration, _ := time.ParseDuration(c.Events.ReconnectMax)
		if minDuration > maxDuration {
			return fmt.Errorf("events reconnect min (%s) cannot be greater than reconnect max (%s)",
				c.Events.ReconnectMin, c.Events.ReconnectMax)
		}
	}

	// Validate credits configuration
	if c.Credits.Target < c.Credits.MinBuffer {
		return fmt.Errorf("credits target (%d) must be greater than or equal to minBuffer (%d)", c.Credits.Target, c.Credits.MinBuffer)
	}
	if c.Credits.FundingToken == "" {
		return fmt.Errorf("credits funding token URL cannot be empty")
	}

	// Validate keystore configuration
	if c.Keystore.Path == "" {
		return fmt.Errorf("keystore path cannot be empty")
	}

	// Validate server configuration
	if c.Server.Addr == "" {
		return fmt.Errorf("server address cannot be empty")
	}
	if c.Server.Rate.RPS < 0 {
		return fmt.Errorf("server rate RPS must be non-negative, got %d", c.Server.Rate.RPS)
	}
	if c.Server.Rate.Burst < 0 {
		return fmt.Errorf("server rate burst must be non-negative, got %d", c.Server.Rate.Burst)
	}
	if c.Server.Rate.Burst > 0 && c.Server.Rate.Burst < c.Server.Rate.RPS {
		return fmt.Errorf("server rate burst (%d) must be greater than or equal to RPS (%d)", c.Server.Rate.Burst, c.Server.Rate.RPS)
	}

	// Validate TLS configuration if provided
	if c.Server.TLS.Cert != "" && c.Server.TLS.Key == "" {
		return fmt.Errorf("server TLS cert specified but key is missing")
	}
	if c.Server.TLS.Key != "" && c.Server.TLS.Cert == "" {
		return fmt.Errorf("server TLS key specified but cert is missing")
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

// GetEventsReconnectMinDuration returns the events reconnect min duration
func (c *Config) GetEventsReconnectMinDuration() time.Duration {
	duration, err := time.ParseDuration(c.Events.ReconnectMin)
	if err != nil {
		// This should not happen if validation passed
		return 1 * time.Second
	}
	return duration
}

// GetEventsReconnectMaxDuration returns the events reconnect max duration
func (c *Config) GetEventsReconnectMaxDuration() time.Duration {
	duration, err := time.ParseDuration(c.Events.ReconnectMax)
	if err != nil {
		// This should not happen if validation passed
		return 60 * time.Second
	}
	return duration
}

// String returns a string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Endpoints: %v, BlockTime: %s, AnchorEvery: %s, AnchorEveryN: %d, GasScheduleID: %s, BatchSize: %d}",
		c.APIV3Endpoints, c.BlockTime, c.AnchorEvery, c.AnchorEveryN, c.Pricing.GasScheduleID, c.Submitter.BatchSize)
}