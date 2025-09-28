package dn

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
)

// Client provides access to the Accumulate Directory Network (DN) registry
type Client struct {
	querier *l0api.Querier
	client  *l0api.Client
	config  *ClientConfig
}

// ClientConfig defines configuration for the DN registry client
type ClientConfig struct {
	// Base DN URL for Accumen registry
	BaseURL string
	// Enable caching of registry data
	EnableCache bool
	// Cache TTL for registry entries
	CacheTTL time.Duration
}

// DefaultClientConfig returns a default DN client configuration
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		BaseURL:     "acc://dn.acme",
		EnableCache: true,
		CacheTTL:    5 * time.Minute,
	}
}

// RegistryPaths defines the standard registry paths for Accumen
type RegistryPaths struct {
	PluginIndex   string
	PluginSpec    string
	OpcodeTables  string
	GasSchedules  string
	ReservedNames string
	Anchors       string
}

// DefaultRegistryPaths returns the default registry paths
func DefaultRegistryPaths() *RegistryPaths {
	return &RegistryPaths{
		PluginIndex:   "acc://dn.acme/accumen/plugins/index",
		PluginSpec:    "acc://dn.acme/accumen/plugins/%s/spec@v%s",
		OpcodeTables:  "acc://dn.acme/accumen/opcode-tables/%s",
		GasSchedules:  "acc://dn.acme/accumen/gas-schedules/%s",
		ReservedNames: "acc://dn.acme/accumen/namespaces/reserved",
		Anchors:       "acc://dn.acme/accumen/anchors",
	}
}

// NewClient creates a new DN registry client
func NewClient(l0Client *l0api.Client, config *ClientConfig) (*Client, error) {
	if l0Client == nil {
		return nil, fmt.Errorf("L0 client cannot be nil")
	}

	if config == nil {
		config = DefaultClientConfig()
	}

	querier := l0api.NewQuerier(l0Client, nil)

	return &Client{
		querier: querier,
		client:  l0Client,
		config:  config,
	}, nil
}

// GetGasSchedule retrieves a gas schedule by ID from the DN registry
func (c *Client) GetGasSchedule(id string) ([]byte, error) {
	if id == "" {
		return nil, fmt.Errorf("gas schedule ID cannot be empty")
	}

	paths := DefaultRegistryPaths()
	gasScheduleURL := fmt.Sprintf(paths.GasSchedules, id)

	accountURL, err := url.Parse(gasScheduleURL)
	if err != nil {
		return nil, fmt.Errorf("invalid gas schedule URL: %w", err)
	}

	ctx := context.Background()
	dataAccount, err := c.querier.QueryDataAccount(ctx, accountURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query gas schedule %s: %w", id, err)
	}

	// Get the entry from the data account (now a single entry, not a slice)
	if dataAccount.Entry == nil {
		return nil, fmt.Errorf("gas schedule %s not found", id)
	}

	// Return the data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return nil, fmt.Errorf("gas schedule %s has no data", id)
	}

	// Concatenate all data chunks (assuming single chunk for now)
	return entryData[0], nil
}

// GetOpcodeTable retrieves an opcode table by ID from the DN registry
func (c *Client) GetOpcodeTable(id string) ([]byte, error) {
	if id == "" {
		return nil, fmt.Errorf("opcode table ID cannot be empty")
	}

	paths := DefaultRegistryPaths()
	opcodeTableURL := fmt.Sprintf(paths.OpcodeTables, id)

	accountURL, err := url.Parse(opcodeTableURL)
	if err != nil {
		return nil, fmt.Errorf("invalid opcode table URL: %w", err)
	}

	ctx := context.Background()
	dataAccount, err := c.querier.QueryDataAccount(ctx, accountURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query opcode table %s: %w", id, err)
	}

	// Get the entry from the data account (now a single entry, not a slice)
	if dataAccount.Entry == nil {
		return nil, fmt.Errorf("opcode table %s not found", id)
	}

	// Return the data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return nil, fmt.Errorf("opcode table %s has no data", id)
	}

	// Concatenate all data chunks (assuming single chunk for now)
	return entryData[0], nil
}

// GetReservedLabels retrieves the list of reserved namespace labels
func (c *Client) GetReservedLabels() ([]string, error) {
	paths := DefaultRegistryPaths()
	reservedURL, err := url.Parse(paths.ReservedNames)
	if err != nil {
		return nil, fmt.Errorf("invalid reserved names URL: %w", err)
	}

	ctx := context.Background()
	dataAccount, err := c.querier.QueryDataAccount(ctx, reservedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query reserved names: %w", err)
	}

	// Get the entry from the data account (now a single entry, not a slice)
	if dataAccount.Entry == nil {
		return nil, fmt.Errorf("reserved names not found")
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return nil, fmt.Errorf("reserved names has no data")
	}
	reservedNames := strings.Split(string(entryData[0]), "\n")

	// Filter out empty lines
	var filteredNames []string
	for _, name := range reservedNames {
		name = strings.TrimSpace(name)
		if name != "" {
			filteredNames = append(filteredNames, name)
		}
	}

	return filteredNames, nil
}

// PutAnchor stores an anchor blob in the DN registry and returns the transaction ID
func (c *Client) PutAnchor(blob []byte) (txid string, err error) {
	if len(blob) == 0 {
		return "", fmt.Errorf("anchor blob cannot be empty")
	}

	// TODO: Implement proper anchor submission using new L0 API
	// The transaction creation API has significantly changed and requires
	// proper implementation with the new protocol structures
	_ = blob // Prevent unused variable error

	return "", fmt.Errorf("PutAnchor not yet implemented for new API")
}

// GetPluginIndex retrieves the plugin index from the DN registry
func (c *Client) GetPluginIndex() ([]string, error) {
	paths := DefaultRegistryPaths()
	indexURL, err := url.Parse(paths.PluginIndex)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin index URL: %w", err)
	}

	ctx := context.Background()
	dataAccount, err := c.querier.QueryDataAccount(ctx, indexURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query plugin index: %w", err)
	}

	// Get the entry from the data account (now a single entry, not a slice)
	if dataAccount.Entry == nil {
		return nil, fmt.Errorf("plugin index not found")
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return nil, fmt.Errorf("plugin index has no data")
	}

	// Parse the plugin names from the entry data
	pluginNames := strings.Split(string(entryData[0]), "\n")

	// Filter out empty lines
	var filteredNames []string
	for _, name := range pluginNames {
		name = strings.TrimSpace(name)
		if name != "" {
			filteredNames = append(filteredNames, name)
		}
	}

	return filteredNames, nil
}

// GetPluginSpec retrieves a plugin specification by name and version
func (c *Client) GetPluginSpec(name, version string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("plugin name cannot be empty")
	}
	if version == "" {
		return nil, fmt.Errorf("plugin version cannot be empty")
	}

	paths := DefaultRegistryPaths()
	specURL := fmt.Sprintf(paths.PluginSpec, name, version)

	accountURL, err := url.Parse(specURL)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin spec URL: %w", err)
	}

	ctx := context.Background()
	dataAccount, err := c.querier.QueryDataAccount(ctx, accountURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query plugin spec %s@v%s: %w", name, version, err)
	}

	// Get the entry from the data account (now a single entry, not a slice)
	if dataAccount.Entry == nil {
		return nil, fmt.Errorf("plugin spec %s@v%s not found", name, version)
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return nil, fmt.Errorf("plugin spec %s@v%s has no data", name, version)
	}

	// Return the data from the entry
	return entryData[0], nil
}

// ListGasSchedules retrieves a list of available gas schedule IDs
func (c *Client) ListGasSchedules() ([]string, error) {
	// TODO: Implement proper directory listing using new L0 API
	// QueryDirectory is not yet implemented in the L0 API client
	return []string{}, fmt.Errorf("ListGasSchedules not yet implemented for new API")
}

// ListOpcodeTables retrieves a list of available opcode table IDs
func (c *Client) ListOpcodeTables() ([]string, error) {
	// TODO: Implement proper directory listing using new L0 API
	// QueryDirectory is not yet implemented in the L0 API client
	return []string{}, fmt.Errorf("ListOpcodeTables not yet implemented for new API")
}

// ValidateRegistryAccess checks if the client has access to the DN registry
func (c *Client) ValidateRegistryAccess() error {
	ctx := context.Background()

	// Try to access the plugin index as a connectivity test
	paths := DefaultRegistryPaths()
	indexURL, err := url.Parse(paths.PluginIndex)
	if err != nil {
		return fmt.Errorf("invalid plugin index URL: %w", err)
	}

	_, err = c.querier.QueryAccount(ctx, indexURL)
	if err != nil {
		return fmt.Errorf("failed to access DN registry: %w", err)
	}

	return nil
}

// GetRegistryStats returns statistics about the DN registry
func (c *Client) GetRegistryStats() (*RegistryStats, error) {
	stats := &RegistryStats{}

	// Count plugins
	plugins, err := c.GetPluginIndex()
	if err == nil {
		stats.PluginCount = len(plugins)
	}

	// Count gas schedules
	gasSchedules, err := c.ListGasSchedules()
	if err == nil {
		stats.GasScheduleCount = len(gasSchedules)
	}

	// Count opcode tables
	opcodeTables, err := c.ListOpcodeTables()
	if err == nil {
		stats.OpcodeTableCount = len(opcodeTables)
	}

	// Count reserved labels
	reservedLabels, err := c.GetReservedLabels()
	if err == nil {
		stats.ReservedLabelCount = len(reservedLabels)
	}

	return stats, nil
}

// RegistryStats contains statistics about the DN registry
type RegistryStats struct {
	PluginCount        int `json:"plugin_count"`
	GasScheduleCount   int `json:"gas_schedule_count"`
	OpcodeTableCount   int `json:"opcode_table_count"`
	ReservedLabelCount int `json:"reserved_label_count"`
}

// Close releases any resources held by the client
func (c *Client) Close() error {
	// No resources to clean up currently
	return nil
}
