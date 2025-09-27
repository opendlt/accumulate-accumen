package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3/jsonrpc"
)

// Schedule defines gas costs and pricing for operations
type Schedule struct {
	ID        string            `json:"id"`
	Version   int               `json:"version"`
	GCR       float64           `json:"gcr"`        // Gas-to-Credits Ratio
	HostCosts map[string]uint64 `json:"host_costs"` // Host function costs
	PerByte   PerByteSchedule   `json:"per_byte"`   // Per-byte operation costs
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// PerByteSchedule defines per-byte costs for different operations
type PerByteSchedule struct {
	Read  uint64 `json:"read"`  // Cost per byte read
	Write uint64 `json:"write"` // Cost per byte written
}

// DefaultSchedule returns a default gas schedule
func DefaultSchedule() *Schedule {
	return &Schedule{
		ID:      "default",
		Version: 1,
		GCR:     150.0, // 150 credits per 1000 gas units
		HostCosts: map[string]uint64{
			"host.get":        1000, // 1000 gas for KV get
			"host.put":        2000, // 2000 gas for KV put
			"host.delete":     1500, // 1500 gas for KV delete
			"host.emit_event": 500,  // 500 gas for emitting event
			"host.stage_op":   3000, // 3000 gas for staging L0 operation
			"host.charge_gas": 100,  // 100 gas for gas charging overhead
			"host.log":        200,  // 200 gas for logging
		},
		PerByte: PerByteSchedule{
			Read:  10, // 10 gas per byte read
			Write: 20, // 20 gas per byte written
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// GetHostCost returns the gas cost for a host function
func (s *Schedule) GetHostCost(name string) uint64 {
	if cost, exists := s.HostCosts[name]; exists {
		return cost
	}
	return 1000 // Default cost if not found
}

// GetGCR returns the Gas-to-Credits Ratio
func (s *Schedule) GetGCR() float64 {
	return s.GCR
}

// GetReadCost returns the per-byte read cost
func (s *Schedule) GetReadCost() uint64 {
	return s.PerByte.Read
}

// GetWriteCost returns the per-byte write cost
func (s *Schedule) GetWriteCost() uint64 {
	return s.PerByte.Write
}

// CalculateCredits converts gas units to credits using GCR
func (s *Schedule) CalculateCredits(gasUsed uint64) uint64 {
	// GCR is credits per 1000 gas units
	return uint64(float64(gasUsed) * s.GCR / 1000.0)
}

// ScheduleProvider provides cached access to gas schedules with automatic refresh
type ScheduleProvider struct {
	mu           sync.RWMutex
	current      *Schedule
	provider     func() (*Schedule, error)
	ttl          time.Duration
	lastRefresh  time.Time
	refreshCount uint64
	errorCount   uint64
	stopping     bool
	stopCh       chan struct{}
}

// NewScheduleProvider creates a new schedule provider with caching and auto-refresh
func NewScheduleProvider(provider func() (*Schedule, error), ttl time.Duration) *ScheduleProvider {
	sp := &ScheduleProvider{
		provider: provider,
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}

	// Load initial schedule
	if schedule, err := provider(); err == nil {
		sp.current = schedule
		sp.lastRefresh = time.Now()
	} else {
		// Use default schedule if initial load fails
		sp.current = DefaultSchedule()
	}

	return sp
}

// Get returns the current schedule, refreshing if necessary
func (sp *ScheduleProvider) Get() *Schedule {
	sp.mu.RLock()
	if sp.current != nil && time.Since(sp.lastRefresh) < sp.ttl {
		defer sp.mu.RUnlock()
		return sp.current
	}
	sp.mu.RUnlock()

	// Need to refresh
	return sp.refresh()
}

// refresh updates the schedule from the provider
func (sp *ScheduleProvider) refresh() *Schedule {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// Double-check after acquiring write lock
	if sp.current != nil && time.Since(sp.lastRefresh) < sp.ttl {
		return sp.current
	}

	// Attempt to refresh
	if newSchedule, err := sp.provider(); err == nil {
		sp.current = newSchedule
		sp.lastRefresh = time.Now()
		sp.refreshCount++
		return sp.current
	} else {
		sp.errorCount++
		// Return current schedule on error, or default if none
		if sp.current != nil {
			return sp.current
		}
		return DefaultSchedule()
	}
}

// StartAutoRefresh starts a background goroutine that periodically refreshes the schedule
func (sp *ScheduleProvider) StartAutoRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(sp.ttl / 2) // Refresh at half the TTL interval
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-sp.stopCh:
				return
			case <-ticker.C:
				if !sp.stopping {
					sp.refresh()
				}
			}
		}
	}()
}

// Stop stops the auto-refresh goroutine
func (sp *ScheduleProvider) Stop() {
	sp.mu.Lock()
	sp.stopping = true
	sp.mu.Unlock()
	close(sp.stopCh)
}

// GetStats returns statistics about the provider
func (sp *ScheduleProvider) GetStats() ProviderStats {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	return ProviderStats{
		CurrentScheduleID: sp.current.ID,
		LastRefresh:       sp.lastRefresh,
		RefreshCount:      sp.refreshCount,
		ErrorCount:        sp.errorCount,
		TTL:               sp.ttl,
	}
}

// ProviderStats contains statistics about the schedule provider
type ProviderStats struct {
	CurrentScheduleID string        `json:"current_schedule_id"`
	LastRefresh       time.Time     `json:"last_refresh"`
	RefreshCount      uint64        `json:"refresh_count"`
	ErrorCount        uint64        `json:"error_count"`
	TTL               time.Duration `json:"ttl"`
}

// LoadFromDN loads a gas schedule from the DN registry
func LoadFromDN(ctx context.Context, client *v3.Client, scheduleID string) (*Schedule, error) {
	if client == nil {
		return nil, fmt.Errorf("DN client is nil")
	}

	if scheduleID == "" {
		return nil, fmt.Errorf("schedule ID cannot be empty")
	}

	// Construct DN registry URL for gas schedule
	scheduleURL := fmt.Sprintf("acc://dn.acme/registry/gas-schedules/%s", scheduleID)

	// Query the DN for the gas schedule
	resp, err := client.Query(ctx, scheduleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query gas schedule from DN: %w", err)
	}

	// Check if the schedule exists
	if resp.Type == "unknown" {
		return nil, fmt.Errorf("gas schedule %s not found in DN registry", scheduleID)
	}

	// Parse the schedule data
	var schedule Schedule
	if err := json.Unmarshal(resp.Data, &schedule); err != nil {
		return nil, fmt.Errorf("failed to parse gas schedule JSON: %w", err)
	}

	// Validate the schedule
	if err := validateSchedule(&schedule); err != nil {
		return nil, fmt.Errorf("invalid gas schedule: %w", err)
	}

	// Set metadata
	schedule.UpdatedAt = time.Now().UTC()
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = schedule.UpdatedAt
	}

	return &schedule, nil
}

// validateSchedule performs basic validation on a gas schedule
func validateSchedule(schedule *Schedule) error {
	if schedule.ID == "" {
		return fmt.Errorf("schedule ID cannot be empty")
	}

	if schedule.GCR <= 0 {
		return fmt.Errorf("GCR must be positive, got %f", schedule.GCR)
	}

	if schedule.HostCosts == nil {
		return fmt.Errorf("host costs map cannot be nil")
	}

	// Validate required host functions have costs
	requiredFunctions := []string{
		"host.get", "host.put", "host.delete",
		"host.emit_event", "host.stage_op", "host.charge_gas",
	}

	for _, fn := range requiredFunctions {
		if cost, exists := schedule.HostCosts[fn]; !exists {
			return fmt.Errorf("missing cost for required host function: %s", fn)
		} else if cost == 0 {
			return fmt.Errorf("host function %s cannot have zero cost", fn)
		}
	}

	// Validate per-byte costs
	if schedule.PerByte.Read == 0 {
		return fmt.Errorf("per-byte read cost cannot be zero")
	}
	if schedule.PerByte.Write == 0 {
		return fmt.Errorf("per-byte write cost cannot be zero")
	}

	return nil
}

// CreateDNProvider creates a schedule provider that loads from DN registry
func CreateDNProvider(client *v3.Client, scheduleID string) func() (*Schedule, error) {
	return func() (*Schedule, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return LoadFromDN(ctx, client, scheduleID)
	}
}

// Cached creates a cached schedule provider with the given TTL
func Cached(provider func() (*Schedule, error), ttl time.Duration) *ScheduleProvider {
	return NewScheduleProvider(provider, ttl)
}

// MockSchedule creates a mock schedule for testing
func MockSchedule(id string, gcr float64) *Schedule {
	schedule := DefaultSchedule()
	schedule.ID = id
	schedule.GCR = gcr
	schedule.Version++
	schedule.UpdatedAt = time.Now().UTC()
	return schedule
}

// CompareSchedules compares two schedules and returns differences
func CompareSchedules(old, new *Schedule) []string {
	var diffs []string

	if old.ID != new.ID {
		diffs = append(diffs, fmt.Sprintf("ID changed: %s -> %s", old.ID, new.ID))
	}

	if old.GCR != new.GCR {
		diffs = append(diffs, fmt.Sprintf("GCR changed: %f -> %f", old.GCR, new.GCR))
	}

	if old.Version != new.Version {
		diffs = append(diffs, fmt.Sprintf("Version changed: %d -> %d", old.Version, new.Version))
	}

	// Compare host costs
	for fn, oldCost := range old.HostCosts {
		if newCost, exists := new.HostCosts[fn]; !exists {
			diffs = append(diffs, fmt.Sprintf("Host function removed: %s", fn))
		} else if oldCost != newCost {
			diffs = append(diffs, fmt.Sprintf("Host cost changed for %s: %d -> %d", fn, oldCost, newCost))
		}
	}

	for fn := range new.HostCosts {
		if _, exists := old.HostCosts[fn]; !exists {
			diffs = append(diffs, fmt.Sprintf("Host function added: %s", fn))
		}
	}

	// Compare per-byte costs
	if old.PerByte.Read != new.PerByte.Read {
		diffs = append(diffs, fmt.Sprintf("Per-byte read cost changed: %d -> %d", old.PerByte.Read, new.PerByte.Read))
	}
	if old.PerByte.Write != new.PerByte.Write {
		diffs = append(diffs, fmt.Sprintf("Per-byte write cost changed: %d -> %d", old.PerByte.Write, new.PerByte.Write))
	}

	return diffs
}
