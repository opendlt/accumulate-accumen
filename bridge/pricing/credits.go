package pricing

import (
	"fmt"
	"math/big"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

// CreditManager handles credit pricing and calculations for Accumulate operations
type CreditManager struct {
	config *CreditConfig
	oracle *PriceOracle
}

// CreditConfig defines credit pricing configuration
type CreditConfig struct {
	// Base cost for transactions (in credits)
	BaseTxCost uint64
	// Cost per byte of transaction data
	ByteCost uint64
	// Cost per signature verification
	SignatureCost uint64
	// Cost multipliers by transaction type
	TypeMultipliers map[protocol.TransactionType]float64
	// Credit to token exchange rate (credits per ACME token)
	ExchangeRate *big.Rat
}

// PriceOracle provides real-time pricing information
type PriceOracle struct {
	acmePrice    *big.Rat // ACME price in USD
	lastUpdate   time.Time
	updateInterval time.Duration
}

// DefaultCreditConfig returns a default credit configuration
func DefaultCreditConfig() *CreditConfig {
	typeMultipliers := make(map[protocol.TransactionType]float64)
	typeMultipliers[protocol.TransactionTypeSendTokens] = 1.0
	typeMultipliers[protocol.TransactionTypeCreateDataAccount] = 10.0
	typeMultipliers[protocol.TransactionTypeWriteData] = 2.0
	typeMultipliers[protocol.TransactionTypeCreateTokenAccount] = 5.0
	typeMultipliers[protocol.TransactionTypeCreateKeyBook] = 5.0
	typeMultipliers[protocol.TransactionTypeCreateKeyPage] = 2.0
	typeMultipliers[protocol.TransactionTypeUpdateKeyPage] = 1.5

	return &CreditConfig{
		BaseTxCost:      100, // 100 credits base cost
		ByteCost:        1,   // 1 credit per byte
		SignatureCost:   50,  // 50 credits per signature
		TypeMultipliers: typeMultipliers,
		ExchangeRate:    big.NewRat(1000, 1), // 1000 credits per ACME token
	}
}

// NewCreditManager creates a new credit manager
func NewCreditManager(config *CreditConfig) *CreditManager {
	if config == nil {
		config = DefaultCreditConfig()
	}

	oracle := &PriceOracle{
		acmePrice:      big.NewRat(1, 100), // $0.01 default
		lastUpdate:     time.Now(),
		updateInterval: 5 * time.Minute,
	}

	return &CreditManager{
		config: config,
		oracle: oracle,
	}
}

// CalculateTransactionCost calculates the credit cost for a transaction
func (cm *CreditManager) CalculateTransactionCost(tx *protocol.Transaction) (uint64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction cannot be nil")
	}

	// Base cost
	cost := cm.config.BaseTxCost

	// Add data size cost
	txSize := uint64(len(tx.Body))
	cost += txSize * cm.config.ByteCost

	// Add signature cost
	cost += uint64(len(tx.Signature)) * cm.config.SignatureCost

	// Apply type multiplier
	txType := tx.Body.Type()
	if multiplier, exists := cm.config.TypeMultipliers[txType]; exists {
		cost = uint64(float64(cost) * multiplier)
	}

	return cost, nil
}

// CalculateEnvelopeCost calculates the total cost for a transaction envelope
func (cm *CreditManager) CalculateEnvelopeCost(envelope *protocol.Envelope) (uint64, error) {
	if envelope == nil {
		return 0, fmt.Errorf("envelope cannot be nil")
	}

	totalCost := uint64(0)

	// Calculate cost for each transaction in the envelope
	for _, tx := range envelope.Transaction {
		txCost, err := cm.CalculateTransactionCost(tx)
		if err != nil {
			return 0, fmt.Errorf("failed to calculate cost for transaction: %w", err)
		}
		totalCost += txCost
	}

	return totalCost, nil
}

// CalculateAccountCreationCost calculates the cost to create a new account
func (cm *CreditManager) CalculateAccountCreationCost(accountType protocol.AccountType) uint64 {
	baseCost := cm.config.BaseTxCost * 10 // Account creation is expensive

	switch accountType {
	case protocol.AccountTypeTokenAccount:
		return baseCost * 5
	case protocol.AccountTypeDataAccount:
		return baseCost * 10
	case protocol.AccountTypeKeyBook:
		return baseCost * 5
	case protocol.AccountTypeKeyPage:
		return baseCost * 2
	default:
		return baseCost
	}
}

// CalculateDataWriteCost calculates the cost to write data
func (cm *CreditManager) CalculateDataWriteCost(dataSize uint64) uint64 {
	return cm.config.BaseTxCost + (dataSize * cm.config.ByteCost * 2) // Data writes cost more per byte
}

// EstimateCreditsNeeded estimates credits needed for common operations
func (cm *CreditManager) EstimateCreditsNeeded(operation string, params map[string]interface{}) (uint64, error) {
	switch operation {
	case "send_tokens":
		return cm.config.BaseTxCost + cm.config.SignatureCost, nil
	case "create_account":
		accountType, ok := params["type"].(protocol.AccountType)
		if !ok {
			return 0, fmt.Errorf("account type required for create_account operation")
		}
		return cm.CalculateAccountCreationCost(accountType), nil
	case "write_data":
		size, ok := params["size"].(uint64)
		if !ok {
			return 0, fmt.Errorf("data size required for write_data operation")
		}
		return cm.CalculateDataWriteCost(size), nil
	default:
		return 0, fmt.Errorf("unknown operation: %s", operation)
	}
}

// ConvertCreditsToACME converts credits to ACME tokens
func (cm *CreditManager) ConvertCreditsToACME(credits uint64) *big.Rat {
	creditsBig := big.NewRat(int64(credits), 1)
	return new(big.Rat).Quo(creditsBig, cm.config.ExchangeRate)
}

// ConvertACMEToCredits converts ACME tokens to credits
func (cm *CreditManager) ConvertACMEToCredits(acme *big.Rat) uint64 {
	credits := new(big.Rat).Mul(acme, cm.config.ExchangeRate)
	return credits.Num().Uint64() / credits.Denom().Uint64()
}

// GetCreditBalance retrieves credit balance for an account URL
func (cm *CreditManager) GetCreditBalance(accountURL *url.URL) (uint64, error) {
	// This would typically query the network, but for now return a placeholder
	// In a real implementation, this would use the L0 API client
	return 0, fmt.Errorf("not implemented: would query network for credit balance")
}

// ValidateCreditsForOperation checks if an account has sufficient credits
func (cm *CreditManager) ValidateCreditsForOperation(accountURL *url.URL, requiredCredits uint64) error {
	balance, err := cm.GetCreditBalance(accountURL)
	if err != nil {
		return fmt.Errorf("failed to get credit balance: %w", err)
	}

	if balance < requiredCredits {
		return fmt.Errorf("insufficient credits: need %d, have %d", requiredCredits, balance)
	}

	return nil
}

// UpdateExchangeRate updates the credit to ACME exchange rate
func (cm *CreditManager) UpdateExchangeRate(newRate *big.Rat) {
	cm.config.ExchangeRate = newRate
}

// GetExchangeRate returns the current exchange rate
func (cm *CreditManager) GetExchangeRate() *big.Rat {
	return new(big.Rat).Set(cm.config.ExchangeRate)
}

// UpdateACMEPrice updates the ACME token price from oracle
func (po *PriceOracle) UpdateACMEPrice(priceUSD *big.Rat) {
	po.acmePrice = priceUSD
	po.lastUpdate = time.Now()
}

// GetACMEPrice returns the current ACME price in USD
func (po *PriceOracle) GetACMEPrice() *big.Rat {
	return new(big.Rat).Set(po.acmePrice)
}

// IsStale checks if the price oracle data is stale
func (po *PriceOracle) IsStale() bool {
	return time.Since(po.lastUpdate) > po.updateInterval
}

// GetPriceAge returns how old the current price data is
func (po *PriceOracle) GetPriceAge() time.Duration {
	return time.Since(po.lastUpdate)
}

// CreditCalculator provides utility functions for credit calculations
type CreditCalculator struct {
	manager *CreditManager
}

// NewCreditCalculator creates a new credit calculator
func NewCreditCalculator(manager *CreditManager) *CreditCalculator {
	return &CreditCalculator{
		manager: manager,
	}
}

// BatchCalculate calculates costs for multiple transactions
func (cc *CreditCalculator) BatchCalculate(transactions []*protocol.Transaction) ([]uint64, uint64, error) {
	costs := make([]uint64, len(transactions))
	totalCost := uint64(0)

	for i, tx := range transactions {
		cost, err := cc.manager.CalculateTransactionCost(tx)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to calculate cost for transaction %d: %w", i, err)
		}
		costs[i] = cost
		totalCost += cost
	}

	return costs, totalCost, nil
}

// OptimizeForCost suggests optimizations to reduce transaction costs
func (cc *CreditCalculator) OptimizeForCost(tx *protocol.Transaction) ([]string, error) {
	var suggestions []string

	// Analyze transaction and suggest optimizations
	txSize := len(tx.Body)
	if txSize > 1000 {
		suggestions = append(suggestions, "Consider reducing transaction data size to lower costs")
	}

	sigCount := len(tx.Signature)
	if sigCount > 1 {
		suggestions = append(suggestions, fmt.Sprintf("Transaction has %d signatures, consider using multi-sig efficiently", sigCount))
	}

	return suggestions, nil
}