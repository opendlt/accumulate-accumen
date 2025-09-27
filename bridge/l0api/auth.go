package l0api

import (
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

// AuthorityPermission defines the level of permissions for a delegated authority
type AuthorityPermission int

const (
	// PermissionNone represents no permissions
	PermissionNone AuthorityPermission = iota
	// PermissionSign allows the authority to sign transactions
	PermissionSign
	// PermissionWriteData allows writing data and signing
	PermissionWriteData
	// PermissionSendTokens allows token transfers and signing
	PermissionSendTokens
	// PermissionUpdateAuth allows updating account authority and all operations
	PermissionUpdateAuth
	// PermissionFull grants all permissions
	PermissionFull
)

// String returns the string representation of the permission level
func (p AuthorityPermission) String() string {
	switch p {
	case PermissionNone:
		return "none"
	case PermissionSign:
		return "sign"
	case PermissionWriteData:
		return "write_data"
	case PermissionSendTokens:
		return "send_tokens"
	case PermissionUpdateAuth:
		return "update_auth"
	case PermissionFull:
		return "full"
	default:
		return "unknown"
	}
}

// toProtocolPermission converts to protocol permission flags
// TODO: Fix for current Accumulate API - AccountAuthority type no longer exists
func (p AuthorityPermission) toProtocolPermission() interface{} {
	// Temporarily return empty interface to allow compilation
	return nil
}

// BuildCreateIdentity creates a transaction builder for creating an ADI (Accumulate Decentralized Identity)
func BuildCreateIdentity(identityURL, keyBookURL, publicKeyHex string) (*build.TransactionBuilder, error) {
	// Parse and validate URLs
	identityURLParsed, err := url.Parse(identityURL)
	if err != nil {
		return nil, fmt.Errorf("invalid identity URL %s: %w", identityURL, err)
	}

	keyBookURLParsed, err := url.Parse(keyBookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid key book URL %s: %w", keyBookURL, err)
	}

	// Validate that key book URL is under the identity
	if !keyBookURLParsed.ParentOf(identityURLParsed) {
		return nil, fmt.Errorf("key book URL %s must be under identity URL %s", keyBookURL, identityURL)
	}

	// Decode public key
	publicKey, err := decodePublicKey(publicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// Create the identity creation transaction
	envelope := build.Transaction().
		For(identityURLParsed).
		Body(&build.CreateIdentity{
			URL:     identityURLParsed,
			KeyBook: keyBookURLParsed,
			KeyHash: publicKey.GetHash(),
		})

	return envelope, nil
}

// BuildCreateKeyBook creates a transaction builder for creating a key book
func BuildCreateKeyBook(keyBookURL, publicKeyHex string) (*build.TransactionBuilder, error) {
	// Parse and validate URLs
	keyBookURLParsed, err := url.Parse(keyBookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid key book URL %s: %w", keyBookURL, err)
	}

	// Decode public key
	publicKey, err := decodePublicKey(publicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// Create the key book creation transaction
	envelope := build.Transaction().
		For(keyBookURLParsed).
		Body(&build.CreateKeyBook{
			URL:     keyBookURLParsed,
			KeyHash: publicKey.GetHash(),
		})

	return envelope, nil
}

// BuildCreateKeyPage creates a transaction builder for creating a key page within a key book
func BuildCreateKeyPage(keyPageURL, publicKeyHex string, creditBalance uint64) (*build.TransactionBuilder, error) {
	// Parse and validate URLs
	keyPageURLParsed, err := url.Parse(keyPageURL)
	if err != nil {
		return nil, fmt.Errorf("invalid key page URL %s: %w", keyPageURL, err)
	}

	// Decode public key
	publicKey, err := decodePublicKey(publicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// Create keys array with the provided key
	keys := []*build.KeySpecParams{{
		KeyHash: publicKey.GetHash(),
	}}

	// Create the key page creation transaction
	envelope := build.Transaction().
		For(keyPageURLParsed).
		Body(&build.CreateKeyPage{
			Keys: keys,
		})

	// Add credit deposit if balance is specified
	if creditBalance > 0 {
		envelope = envelope.Body(&build.AddCredits{
			Recipient: keyPageURLParsed,
			Amount:    build.BigInt(creditBalance),
		})
	}

	return envelope, nil
}

// BuildUpdateAccountAuth creates a transaction builder for updating account authority to delegate to a contract
func BuildUpdateAccountAuthDelegate(accountURL, contractURL, keyBookURL string, permissions ...AuthorityPermission) (*build.TransactionBuilder, error) {
	// Parse and validate URLs
	accountURLParsed, err := url.Parse(accountURL)
	if err != nil {
		return nil, fmt.Errorf("invalid account URL %s: %w", accountURL, err)
	}

	contractURLParsed, err := url.Parse(contractURL)
	if err != nil {
		return nil, fmt.Errorf("invalid contract URL %s: %w", contractURL, err)
	}

	keyBookURLParsed, err := url.Parse(keyBookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid key book URL %s: %w", keyBookURL, err)
	}

	// Determine the permission level (use highest if multiple provided)
	var maxPermission AuthorityPermission = PermissionNone
	for _, perm := range permissions {
		if perm > maxPermission {
			maxPermission = perm
		}
	}

	// If no permissions specified, default to write data permission
	if maxPermission == PermissionNone {
		maxPermission = PermissionWriteData
	}

	// Convert to protocol authority
	authority := maxPermission.toProtocolPermission()

	// Create the account authority update transaction
	envelope := build.Transaction().
		For(accountURLParsed).
		Body(&build.UpdateAccountAuth{
			Operations: []build.AccountAuthOperation{
				// Add the existing key book authority (preserve existing authority)
				&build.AddAccountAuthorityOperation{
					Authority: keyBookURLParsed,
					AuthorityType: protocol.AccountAuthority{
						TransactionTypes: []protocol.TransactionType{
							protocol.TransactionTypeUpdateAccountAuth, // Full authority for the key book
							protocol.TransactionTypeWriteData,
							protocol.TransactionTypeSendTokens,
							protocol.TransactionTypeCreateDataAccount,
							protocol.TransactionTypeCreateTokenAccount,
						},
					},
				},
				// Add the contract as a delegated authority
				&build.AddAccountAuthorityOperation{
					Authority:     contractURLParsed,
					AuthorityType: authority,
				},
			},
		})

	return envelope, nil
}

// BuildRemoveContractDelegate creates a transaction builder for removing a contract's delegated authority
func BuildRemoveContractDelegate(accountURL, contractURL string) (*build.TransactionBuilder, error) {
	// Parse and validate URLs
	accountURLParsed, err := url.Parse(accountURL)
	if err != nil {
		return nil, fmt.Errorf("invalid account URL %s: %w", accountURL, err)
	}

	contractURLParsed, err := url.Parse(contractURL)
	if err != nil {
		return nil, fmt.Errorf("invalid contract URL %s: %w", contractURL, err)
	}

	// Create the account authority update transaction to remove the contract
	envelope := build.Transaction().
		For(accountURLParsed).
		Body(&build.UpdateAccountAuth{
			Operations: []build.AccountAuthOperation{
				&build.RemoveAccountAuthorityOperation{
					Authority: contractURLParsed,
				},
			},
		})

	return envelope, nil
}

// BuildAddCreditsToKeyPage creates a transaction builder for adding credits to a key page
func BuildAddCreditsToKeyPage(keyPageURL string, creditAmount uint64) (*build.TransactionBuilder, error) {
	// Parse and validate URL
	keyPageURLParsed, err := url.Parse(keyPageURL)
	if err != nil {
		return nil, fmt.Errorf("invalid key page URL %s: %w", keyPageURL, err)
	}

	// Use the builder helper with nil token source (will use default oracle)
	return BuildAddCredits(keyPageURLParsed, nil, creditAmount, "Add credits to key page"), nil
}

// BuildCreateDataAccount creates a transaction builder for creating a data account
func BuildCreateDataAccount(dataAccountURL, keyBookURL string) (*build.TransactionBuilder, error) {
	// Parse and validate URLs
	dataAccountURLParsed, err := url.Parse(dataAccountURL)
	if err != nil {
		return nil, fmt.Errorf("invalid data account URL %s: %w", dataAccountURL, err)
	}

	keyBookURLParsed, err := url.Parse(keyBookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid key book URL %s: %w", keyBookURL, err)
	}

	// Create the data account creation transaction
	envelope := build.Transaction().
		For(dataAccountURLParsed).
		Body(&build.CreateDataAccount{
			URL:     dataAccountURLParsed,
			KeyBook: keyBookURLParsed,
		})

	return envelope, nil
}

// BuildWriteDataToAccount creates a transaction builder for writing data to a data account
func BuildWriteDataToAccount(dataAccountURL string, data []byte) (*build.TransactionBuilder, error) {
	// Parse and validate URL
	dataAccountURLParsed, err := url.Parse(dataAccountURL)
	if err != nil {
		return nil, fmt.Errorf("invalid data account URL %s: %w", dataAccountURL, err)
	}

	// Use the builder helper
	return BuildWriteData(dataAccountURLParsed, data, "Write data to account", nil), nil
}

// Helper function to decode a hex-encoded public key
// TODO: Fix for current Accumulate API - protocol.PublicKey and UnmarshalPublicKeyHex no longer exist
func decodePublicKey(publicKeyHex string) ([]byte, error) {
	// Remove 0x prefix if present
	if len(publicKeyHex) > 2 && publicKeyHex[:2] == "0x" {
		publicKeyHex = publicKeyHex[2:]
	}

	// For now, just return error to allow compilation
	return nil, fmt.Errorf("decodePublicKey not implemented for current Accumulate API")
}

// ContractDelegationConfig holds configuration for contract delegation setup
type ContractDelegationConfig struct {
	// Identity configuration
	IdentityURL string // e.g., "acc://mycompany.acme"
	KeyBookURL  string // e.g., "acc://mycompany.acme/book"
	KeyPageURL  string // e.g., "acc://mycompany.acme/book/1"

	// Contract configuration
	ContractURL string // e.g., "acc://mycontract.mycompany.acme"

	// Data account configuration (optional)
	DataAccountURL string // e.g., "acc://mydata.mycompany.acme"

	// Authority configuration
	PublicKeyHex   string                // Hex-encoded public key
	Permissions    []AuthorityPermission // Contract permissions
	InitialCredits uint64                // Credits to add to key page
}

// ValidateConfig validates the contract delegation configuration
func (c *ContractDelegationConfig) ValidateConfig() error {
	if c.IdentityURL == "" {
		return fmt.Errorf("identity URL is required")
	}
	if c.KeyBookURL == "" {
		return fmt.Errorf("key book URL is required")
	}
	if c.KeyPageURL == "" {
		return fmt.Errorf("key page URL is required")
	}
	if c.ContractURL == "" {
		return fmt.Errorf("contract URL is required")
	}
	if c.PublicKeyHex == "" {
		return fmt.Errorf("public key hex is required")
	}

	// Validate URL relationships
	identityURL, err := url.Parse(c.IdentityURL)
	if err != nil {
		return fmt.Errorf("invalid identity URL: %w", err)
	}

	keyBookURL, err := url.Parse(c.KeyBookURL)
	if err != nil {
		return fmt.Errorf("invalid key book URL: %w", err)
	}

	if !keyBookURL.ParentOf(identityURL) {
		return fmt.Errorf("key book URL must be under identity URL")
	}

	keyPageURL, err := url.Parse(c.KeyPageURL)
	if err != nil {
		return fmt.Errorf("invalid key page URL: %w", err)
	}

	if !keyPageURL.ParentOf(keyBookURL) {
		return fmt.Errorf("key page URL must be under key book URL")
	}

	// Validate contract URL is under identity
	contractURL, err := url.Parse(c.ContractURL)
	if err != nil {
		return fmt.Errorf("invalid contract URL: %w", err)
	}

	if !contractURL.ParentOf(identityURL) {
		return fmt.Errorf("contract URL should be under identity URL")
	}

	return nil
}

// BuildFullDelegationSetup creates all the necessary transactions for setting up contract delegation
// Returns a slice of envelope builders that should be executed in order
func BuildFullDelegationSetup(config *ContractDelegationConfig) ([]*build.TransactionBuilder, error) {
	// Validate configuration
	if err := config.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	var envelopes []*build.TransactionBuilder

	// 1. Create Identity
	identityEnv, err := BuildCreateIdentity(config.IdentityURL, config.KeyBookURL, config.PublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to build create identity: %w", err)
	}
	envelopes = append(envelopes, identityEnv)

	// 2. Create Key Book
	keyBookEnv, err := BuildCreateKeyBook(config.KeyBookURL, config.PublicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to build create key book: %w", err)
	}
	envelopes = append(envelopes, keyBookEnv)

	// 3. Create Key Page with credits
	keyPageEnv, err := BuildCreateKeyPage(config.KeyPageURL, config.PublicKeyHex, config.InitialCredits)
	if err != nil {
		return nil, fmt.Errorf("failed to build create key page: %w", err)
	}
	envelopes = append(envelopes, keyPageEnv)

	// 4. Create Data Account (optional)
	if config.DataAccountURL != "" {
		dataAccountEnv, err := BuildCreateDataAccount(config.DataAccountURL, config.KeyBookURL)
		if err != nil {
			return nil, fmt.Errorf("failed to build create data account: %w", err)
		}
		envelopes = append(envelopes, dataAccountEnv)

		// 5. Update Data Account Authority to include contract
		updateDataAuthEnv, err := BuildUpdateAccountAuthDelegate(
			config.DataAccountURL,
			config.ContractURL,
			config.KeyBookURL,
			config.Permissions...,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to build update data account auth: %w", err)
		}
		envelopes = append(envelopes, updateDataAuthEnv)
	}

	// 6. Update Identity Authority to include contract
	updateIdentityAuthEnv, err := BuildUpdateAccountAuthDelegate(
		config.IdentityURL,
		config.ContractURL,
		config.KeyBookURL,
		config.Permissions...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build update identity auth: %w", err)
	}
	envelopes = append(envelopes, updateIdentityAuthEnv)

	return envelopes, nil
}
