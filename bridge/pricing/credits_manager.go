package pricing

import (
	"context"
	"fmt"
	"math/big"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
	"github.com/opendlt/accumulate-accumen/internal/l0"
)

// Manager manages automatic credit top-ups for L1 operations to maintain consistent funding
type Manager struct {
	sched   *ScheduleProvider
	querier *l0api.Querier
	rr      *l0.RoundRobin
}

// NewManager creates a new credits manager
func NewManager(sched *ScheduleProvider, querier *l0api.Querier, rr *l0.RoundRobin) *Manager {
	return &Manager{
		sched:   sched,
		querier: querier,
		rr:      rr,
	}
}

// EnsureCredits ensures a key page has sufficient credits, building an AddCredits transaction if needed
// Returns:
// - *build.TransactionBuilder: The transaction to add credits (nil if sufficient credits already exist)
// - bool: Whether a transaction was built
// - error: Any error that occurred
func (m *Manager) EnsureCredits(ctx context.Context, page *url.URL, want uint64, fundFrom *url.URL) (*build.TransactionBuilder, bool, error) {
	if page == nil {
		return nil, false, fmt.Errorf("key page URL cannot be nil")
	}

	if fundFrom == nil {
		return nil, false, fmt.Errorf("funding token URL cannot be nil")
	}

	// Validate URLs
	if !accutil.IsKeyPage(page) {
		return nil, false, fmt.Errorf("invalid key page URL: %s", page)
	}

	// Query current credits on the key page
	currentCredits, err := m.querier.QueryCredits(ctx, page)
	if err != nil {
		return nil, false, fmt.Errorf("failed to query credits for page %s: %w", page, err)
	}

	// Check if we already have sufficient credits
	if currentCredits >= want {
		return nil, false, nil // No transaction needed
	}

	// Calculate how many credits we need to add
	creditsNeeded := want - currentCredits

	// Get current schedule for GCR (Gas-to-Credits Ratio)
	schedule := m.sched.Get()
	if schedule == nil {
		return nil, false, fmt.Errorf("failed to get pricing schedule")
	}

	// Calculate ACME required based on GCR
	// GCR is credits per 1000 gas units, but for credits->ACME conversion we need to invert it
	// Standard conversion: credits / GCR = ACME tokens needed (scaled appropriately)
	acmeNeededFloat := float64(creditsNeeded) / schedule.GCR
	acmeNeeded := new(big.Int)
	acmeNeeded.SetUint64(uint64(acmeNeededFloat * 1e8)) // Convert to precision units (1e8 = 100,000,000 precision)

	// Build AddCredits transaction using builder helper
	envelope := l0api.BuildAddCredits(page, fundFrom, creditsNeeded, "accumen-topup")

	return envelope, true, nil
}

// EstimateCreditsForGas estimates how many credits are needed for a given gas amount
func (m *Manager) EstimateCreditsForGas(gasAmount uint64) uint64 {
	schedule := m.sched.Get()
	if schedule == nil {
		// Use default if no schedule available
		return gasAmount / 10 // Conservative estimate
	}

	return schedule.CalculateCredits(gasAmount)
}

// EstimateACMEForCredits estimates how much ACME is needed to purchase a given amount of credits
func (m *Manager) EstimateACMEForCredits(credits uint64) *big.Int {
	schedule := m.sched.Get()
	if schedule == nil {
		// Use conservative estimate if no schedule available
		// Assume 1 ACME = 1000 credits as a fallback
		acmeNeeded := new(big.Int)
		acmeNeeded.SetUint64(credits / 1000 * 1e8) // Convert to precision units
		return acmeNeeded
	}

	// Calculate ACME required based on GCR
	acmeNeededFloat := float64(credits) / schedule.GCR
	acmeNeeded := new(big.Int)
	acmeNeeded.SetUint64(uint64(acmeNeededFloat * 1e8)) // Convert to precision units

	return acmeNeeded
}

// CheckMultiplePages checks credits for multiple key pages and returns which ones need funding
func (m *Manager) CheckMultiplePages(ctx context.Context, pages []*url.URL, minCredits uint64) ([]PageStatus, error) {
	if len(pages) == 0 {
		return nil, nil
	}

	results := make([]PageStatus, len(pages))
	for i, page := range pages {
		if page == nil {
			results[i] = PageStatus{
				Page:         nil,
				Error:        fmt.Errorf("page URL cannot be nil"),
				NeedsFunding: false,
			}
			continue
		}

		credits, err := m.querier.QueryCredits(ctx, page)
		if err != nil {
			results[i] = PageStatus{
				Page:           page,
				CurrentCredits: 0,
				Error:          fmt.Errorf("failed to query credits: %w", err),
				NeedsFunding:   true, // Assume it needs funding if we can't check
			}
			continue
		}

		results[i] = PageStatus{
			Page:           page,
			CurrentCredits: credits,
			NeedsFunding:   credits < minCredits,
			Error:          nil,
		}
	}

	return results, nil
}

// PageStatus represents the credit status of a key page
type PageStatus struct {
	Page           *url.URL `json:"page"`
	CurrentCredits uint64   `json:"currentCredits"`
	NeedsFunding   bool     `json:"needsFunding"`
	Error          error    `json:"error,omitempty"`
}

// BuildBatchTopUp creates multiple AddCredits transactions for pages that need funding
func (m *Manager) BuildBatchTopUp(ctx context.Context, statuses []PageStatus, targetCredits uint64, fundFrom *url.URL) ([]*build.TransactionBuilder, error) {
	if fundFrom == nil {
		return nil, fmt.Errorf("funding token URL cannot be nil")
	}

	var envelopes []*build.TransactionBuilder

	for _, status := range statuses {
		if !status.NeedsFunding || status.Error != nil {
			continue // Skip pages that don't need funding or have errors
		}

		envelope, built, err := m.EnsureCredits(ctx, status.Page, targetCredits, fundFrom)
		if err != nil {
			return nil, fmt.Errorf("failed to build credits transaction for page %s: %w", status.Page, err)
		}

		if built && envelope != nil {
			envelopes = append(envelopes, envelope)
		}
	}

	return envelopes, nil
}

// GetStats returns statistics about credit management
func (m *Manager) GetStats() ManagerStats {
	schedule := m.sched.Get()
	var currentGCR float64
	if schedule != nil {
		currentGCR = schedule.GCR
	}

	return ManagerStats{
		CurrentGCR: currentGCR,
		ScheduleID: func() string {
			if schedule != nil {
				return schedule.ID
			}
			return "unknown"
		}(),
		HasQuerier:    m.querier != nil,
		HasRoundRobin: m.rr != nil,
	}
}

// ManagerStats contains statistics about the credits manager
type ManagerStats struct {
	CurrentGCR    float64 `json:"currentGCR"`
	ScheduleID    string  `json:"scheduleID"`
	HasQuerier    bool    `json:"hasQuerier"`
	HasRoundRobin bool    `json:"hasRoundRobin"`
}

// ValidateConfiguration validates the manager's configuration
func (m *Manager) ValidateConfiguration() error {
	if m.sched == nil {
		return fmt.Errorf("schedule provider cannot be nil")
	}

	if m.querier == nil {
		return fmt.Errorf("L0 API querier cannot be nil")
	}

	if m.rr == nil {
		return fmt.Errorf("round robin endpoint manager cannot be nil")
	}

	// Test that we can get a schedule
	schedule := m.sched.Get()
	if schedule == nil {
		return fmt.Errorf("failed to get pricing schedule")
	}

	if schedule.GCR <= 0 {
		return fmt.Errorf("invalid GCR in pricing schedule: %f", schedule.GCR)
	}

	return nil
}

// SetScheduleProvider updates the schedule provider (for testing or reconfiguration)
func (m *Manager) SetScheduleProvider(sched *ScheduleProvider) {
	m.sched = sched
}

// GetCurrentGCR returns the current Gas-to-Credits Ratio
func (m *Manager) GetCurrentGCR() float64 {
	schedule := m.sched.Get()
	if schedule == nil {
		return 0
	}
	return schedule.GCR
}

// IsHealthy returns whether the credits manager is in a healthy state
func (m *Manager) IsHealthy() bool {
	// Check if we have all required components
	if m.sched == nil || m.querier == nil || m.rr == nil {
		return false
	}

	// Check if we can get a valid schedule
	schedule := m.sched.Get()
	if schedule == nil || schedule.GCR <= 0 {
		return false
	}

	// Check if round robin has healthy endpoints
	endpoints := m.rr.GetEndpoints()
	hasHealthyEndpoint := false
	for _, ep := range endpoints {
		if ep.Healthy {
			hasHealthyEndpoint = true
			break
		}
	}

	return hasHealthyEndpoint
}

// QuoteForGas calculates the credit cost and ACME equivalent for a given gas amount
func QuoteForGas(gas uint64) (credits uint64, acmeDecimal string) {
	// Use default schedule for standalone function
	schedule := DefaultSchedule()

	// Calculate credits using the Gas-to-Credits Ratio (GCR)
	credits = schedule.CalculateCredits(gas)

	// Calculate ACME equivalent using the Credits-to-ACME Rate (CAR)
	car := schedule.GetCAR()
	if car == 0 {
		acmeDecimal = "0.000000"
		return
	}

	// ACME = credits / CAR
	creditsDecimal := new(big.Float).SetUint64(credits)
	carDecimal := new(big.Float).SetFloat64(car)

	acmeFloat := new(big.Float).Quo(creditsDecimal, carDecimal)

	// Format to 6 decimal places for ACME precision
	acmeDecimal = acmeFloat.Text('f', 6)

	return credits, acmeDecimal
}

// QuoteForGasWithManager calculates the credit cost and ACME equivalent using this manager's schedule
func (m *Manager) QuoteForGasWithManager(gas uint64) (credits uint64, acmeDecimal string) {
	var schedule *Schedule
	if m.sched != nil {
		schedule = m.sched.Get()
	}
	if schedule == nil {
		schedule = DefaultSchedule()
	}

	// Calculate credits using the Gas-to-Credits Ratio (GCR)
	credits = schedule.CalculateCredits(gas)

	// Calculate ACME equivalent using the Credits-to-ACME Rate (CAR)
	car := schedule.GetCAR()
	if car == 0 {
		acmeDecimal = "0.000000"
		return
	}

	// ACME = credits / CAR
	creditsDecimal := new(big.Float).SetUint64(credits)
	carDecimal := new(big.Float).SetFloat64(car)

	acmeFloat := new(big.Float).Quo(creditsDecimal, carDecimal)

	// Format to 6 decimal places for ACME precision
	acmeDecimal = acmeFloat.Text('f', 6)

	return credits, acmeDecimal
}
