package sequencer

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/outputs"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/gas"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
)

// Config defines the configuration for the Accumen sequencer
type Config struct {
	// Sequencer identity and networking
	SequencerID string `json:"sequencer_id"`
	ListenAddr  string `json:"listen_addr"`
	MaxPeers    int    `json:"max_peers"`

	// Block production settings
	BlockTime        time.Duration `json:"block_time"`
	MaxBlockSize     uint64        `json:"max_block_size"`
	MaxTransactions  int           `json:"max_transactions"`
	MinConfirmations int           `json:"min_confirmations"`

	// Mempool configuration
	Mempool MempoolConfig `json:"mempool"`

	// Execution engine configuration
	Execution ExecutionConfig `json:"execution"`

	// L0 bridge configuration
	Bridge BridgeConfig `json:"bridge"`

	// Registry configuration
	Registry RegistryConfig `json:"registry"`

	// Monitoring and logging
	MetricsAddr   string `json:"metrics_addr"`
	LogLevel      string `json:"log_level"`
	EnableProfile bool   `json:"enable_profile"`
	ProfileAddr   string `json:"profile_addr"`
}

// MempoolConfig defines mempool-specific configuration
type MempoolConfig struct {
	MaxSize          int           `json:"max_size"`
	MaxTxSize        uint64        `json:"max_tx_size"`
	TTL              time.Duration `json:"ttl"`
	PriceLimit       uint64        `json:"price_limit"`
	AccountLimit     int           `json:"account_limit"`
	GlobalLimit      int           `json:"global_limit"`
	EnablePriority   bool          `json:"enable_priority"`
	RebroadcastDelay time.Duration `json:"rebroadcast_delay"`
}

// ExecutionConfig defines execution engine configuration
type ExecutionConfig struct {
	// WASM runtime settings
	Runtime runtime.Config `json:"runtime"`

	// Gas configuration
	Gas gas.Config `json:"gas"`

	// Parallel execution settings
	WorkerCount    int  `json:"worker_count"`
	EnableParallel bool `json:"enable_parallel"`

	// State management
	StateDir     string `json:"state_dir"`
	SnapshotDir  string `json:"snapshot_dir"`
	EnablePrune  bool   `json:"enable_prune"`
	PruneHistory int    `json:"prune_history"`
}

// BridgeConfig defines L0 bridge configuration
type BridgeConfig struct {
	// L0 API client settings
	Client l0api.ClientConfig `json:"client"`

	// Credit pricing
	Pricing pricing.CreditConfig `json:"pricing"`

	// Output staging and submission
	Stager    outputs.StagerConfig    `json:"stager"`
	Submitter outputs.SubmitterConfig `json:"submitter"`

	// Bridge operation settings
	EnableBridge     bool          `json:"enable_bridge"`
	SubmissionDelay  time.Duration `json:"submission_delay"`
	BatchSize        int           `json:"batch_size"`
	ConfirmationTime time.Duration `json:"confirmation_time"`
}

// RegistryConfig defines DN registry configuration
type RegistryConfig struct {
	Endpoint    string        `json:"endpoint"`
	EnableCache bool          `json:"enable_cache"`
	CacheTTL    time.Duration `json:"cache_ttl"`
	RefreshRate time.Duration `json:"refresh_rate"`
}

// DefaultConfig returns a default sequencer configuration
func DefaultConfig() *Config {
	return &Config{
		SequencerID: "accumen-seq-1",
		ListenAddr:  "0.0.0.0:8080",
		MaxPeers:    50,

		BlockTime:        2 * time.Second,
		MaxBlockSize:     1024 * 1024, // 1MB
		MaxTransactions:  1000,
		MinConfirmations: 1,

		Mempool: MempoolConfig{
			MaxSize:          10000,
			MaxTxSize:        64 * 1024, // 64KB
			TTL:              10 * time.Minute,
			PriceLimit:       100, // Minimum 100 credits
			AccountLimit:     100, // Max 100 pending per account
			GlobalLimit:      5000,
			EnablePriority:   true,
			RebroadcastDelay: 30 * time.Second,
		},

		Execution: ExecutionConfig{
			Runtime: *runtime.DefaultConfig(),
			Gas:     *gas.DefaultGasConfig(),

			WorkerCount:    4,
			EnableParallel: true,

			StateDir:     "./data/state",
			SnapshotDir:  "./data/snapshots",
			EnablePrune:  true,
			PruneHistory: 1000,
		},

		Bridge: BridgeConfig{
			Client:    *l0api.DefaultClientConfig("https://mainnet.accumulatenetwork.io"),
			Pricing:   *pricing.DefaultCreditConfig(),
			Stager:    *outputs.DefaultStagerConfig(),
			Submitter: *outputs.DefaultSubmitterConfig(),

			EnableBridge:     true,
			SubmissionDelay:  5 * time.Second,
			BatchSize:        50,
			ConfirmationTime: 30 * time.Second,
		},

		Registry: RegistryConfig{
			Endpoint:    "https://mainnet.accumulatenetwork.io",
			EnableCache: true,
			CacheTTL:    5 * time.Minute,
			RefreshRate: time.Hour,
		},

		MetricsAddr:   ":9090",
		LogLevel:      "info",
		EnableProfile: false,
		ProfileAddr:   ":6060",
	}
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// SaveConfig saves configuration to a JSON file
func (c *Config) SaveConfig(path string) error {
	if path == "" {
		return fmt.Errorf("config path cannot be empty")
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}

	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.SequencerID == "" {
		return fmt.Errorf("sequencer_id cannot be empty")
	}

	if c.ListenAddr == "" {
		return fmt.Errorf("listen_addr cannot be empty")
	}

	if c.BlockTime <= 0 {
		return fmt.Errorf("block_time must be positive")
	}

	if c.MaxBlockSize == 0 {
		return fmt.Errorf("max_block_size must be positive")
	}

	if c.MaxTransactions <= 0 {
		return fmt.Errorf("max_transactions must be positive")
	}

	// Validate mempool config
	if err := c.Mempool.Validate(); err != nil {
		return fmt.Errorf("mempool config invalid: %w", err)
	}

	// Validate execution config
	if err := c.Execution.Validate(); err != nil {
		return fmt.Errorf("execution config invalid: %w", err)
	}

	// Validate bridge config
	if err := c.Bridge.Validate(); err != nil {
		return fmt.Errorf("bridge config invalid: %w", err)
	}

	// Validate registry config
	if err := c.Registry.Validate(); err != nil {
		return fmt.Errorf("registry config invalid: %w", err)
	}

	return nil
}

// Validate validates the mempool configuration
func (m *MempoolConfig) Validate() error {
	if m.MaxSize <= 0 {
		return fmt.Errorf("max_size must be positive")
	}

	if m.MaxTxSize == 0 {
		return fmt.Errorf("max_tx_size must be positive")
	}

	if m.TTL <= 0 {
		return fmt.Errorf("ttl must be positive")
	}

	if m.AccountLimit <= 0 {
		return fmt.Errorf("account_limit must be positive")
	}

	if m.GlobalLimit <= 0 {
		return fmt.Errorf("global_limit must be positive")
	}

	return nil
}

// Validate validates the execution configuration
func (e *ExecutionConfig) Validate() error {
	if e.WorkerCount <= 0 {
		return fmt.Errorf("worker_count must be positive")
	}

	if e.StateDir == "" {
		return fmt.Errorf("state_dir cannot be empty")
	}

	if e.SnapshotDir == "" {
		return fmt.Errorf("snapshot_dir cannot be empty")
	}

	if e.PruneHistory < 0 {
		return fmt.Errorf("prune_history cannot be negative")
	}

	return nil
}

// Validate validates the bridge configuration
func (b *BridgeConfig) Validate() error {
	if b.Client.Endpoint == "" {
		return fmt.Errorf("client endpoint cannot be empty")
	}

	if b.SubmissionDelay < 0 {
		return fmt.Errorf("submission_delay cannot be negative")
	}

	if b.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be positive")
	}

	if b.ConfirmationTime <= 0 {
		return fmt.Errorf("confirmation_time must be positive")
	}

	return nil
}

// Validate validates the registry configuration
func (r *RegistryConfig) Validate() error {
	if r.Endpoint == "" {
		return fmt.Errorf("endpoint cannot be empty")
	}

	if r.CacheTTL <= 0 {
		return fmt.Errorf("cache_ttl must be positive")
	}

	if r.RefreshRate <= 0 {
		return fmt.Errorf("refresh_rate must be positive")
	}

	return nil
}

// GetDataDir returns the base data directory
func (c *Config) GetDataDir() string {
	return "./data"
}

// GetStateDir returns the state directory
func (c *Config) GetStateDir() string {
	return c.Execution.StateDir
}

// GetSnapshotDir returns the snapshot directory
func (c *Config) GetSnapshotDir() string {
	return c.Execution.SnapshotDir
}

// IsSequencer returns true if this node is configured as a sequencer
func (c *Config) IsSequencer() bool {
	return true // For now, this config is always for sequencer mode
}

// GetNetworkID returns the network identifier
func (c *Config) GetNetworkID() string {
	return "accumen-testnet-1"
}

// Clone creates a deep copy of the configuration
func (c *Config) Clone() *Config {
	data, _ := json.Marshal(c)
	clone := &Config{}
	json.Unmarshal(data, clone)
	return clone
}
