package dn

import (
	"context"
	"fmt"
	"strings"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

// NamespacePaths defines configurable paths for namespace management
type NamespacePaths struct {
	// Path to the reserved namespaces list in the DN
	ReservedNamespaces string
	// Path template for contract registry entries (should include %s for contract URL)
	ContractRegistry string
}

// DefaultNamespacePaths returns default paths for namespace management
func DefaultNamespacePaths() *NamespacePaths {
	return &NamespacePaths{
		ReservedNamespaces: "acc://dn.acme/accumen/namespaces/reserved",
		ContractRegistry:   "acc://dn.acme/accumen/contracts/%s",
	}
}

// NamespaceManager handles namespace validation and contract authorization
type NamespaceManager struct {
	querier *l0api.Querier
	paths   *NamespacePaths
}

// NewNamespaceManager creates a new namespace manager
func NewNamespaceManager(querier *l0api.Querier, paths *NamespacePaths) *NamespaceManager {
	if paths == nil {
		paths = DefaultNamespacePaths()
	}

	return &NamespaceManager{
		querier: querier,
		paths:   paths,
	}
}

// IsReservedNamespace checks if the given namespace label is reserved
// The namespace label is typically extracted from contract URLs like acc://mycontract.accumen
// where "accumen" would be the namespace label to check
func (nm *NamespaceManager) IsReservedNamespace(ctx context.Context, namespace string) (bool, error) {
	if namespace == "" {
		return false, fmt.Errorf("namespace cannot be empty")
	}

	// Query the reserved namespaces list from DN
	reservedURL, err := url.Parse(nm.paths.ReservedNamespaces)
	if err != nil {
		return false, fmt.Errorf("invalid reserved namespaces URL: %w", err)
	}

	dataAccount, err := nm.querier.QueryDataAccount(ctx, reservedURL)
	if err != nil {
		// If the reserved namespaces list doesn't exist, assume no restrictions
		return false, nil
	}

	// Get the entry from the data account (now a single entry, not a slice)
	if dataAccount.Entry == nil {
		// No reserved namespaces configured
		return false, nil
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return false, nil
	}

	// Parse the reserved namespaces from the entry data
	reservedNamespaces := strings.Split(string(entryData[0]), "\n")

	// Check if the namespace is in the reserved list
	for _, reserved := range reservedNamespaces {
		reserved = strings.TrimSpace(reserved)
		if reserved != "" && strings.EqualFold(reserved, namespace) {
			return true, nil
		}
	}

	return false, nil
}

// ContractAllowed checks if a contract is allowed to be deployed at the given URL
// It validates namespace restrictions and checks the contract registry for authorization
func (nm *NamespaceManager) ContractAllowed(ctx context.Context, contractURL string) (bool, string, error) {
	if contractURL == "" {
		return false, "contract URL cannot be empty", fmt.Errorf("contract URL cannot be empty")
	}

	// Parse the contract URL
	parsedURL, err := url.Parse(contractURL)
	if err != nil {
		return false, "invalid contract URL format", fmt.Errorf("invalid contract URL: %w", err)
	}

	// Extract namespace from URL (e.g., "accumen" from "acc://mycontract.accumen")
	authority := parsedURL.Authority
	parts := strings.Split(authority, ".")
	if len(parts) < 2 {
		// No namespace specified, allow deployment
		return true, "", nil
	}

	namespace := parts[len(parts)-1] // Get the last part as namespace

	// Check if the namespace is reserved
	isReserved, err := nm.IsReservedNamespace(ctx, namespace)
	if err != nil {
		return false, "failed to check reserved namespace", fmt.Errorf("failed to check reserved namespace: %w", err)
	}

	if !isReserved {
		// Namespace is not reserved, allow deployment
		return true, "", nil
	}

	// Namespace is reserved, check contract registry for authorization
	allowed, reason, err := nm.checkContractRegistry(ctx, contractURL)
	if err != nil {
		return false, "failed to verify contract authorization", fmt.Errorf("failed to verify contract authorization: %w", err)
	}

	if !allowed {
		return false, fmt.Sprintf("contract not authorized for reserved namespace '%s': %s", namespace, reason), nil
	}

	return true, "", nil
}

// checkContractRegistry verifies if a contract is authorized in the registry
func (nm *NamespaceManager) checkContractRegistry(ctx context.Context, contractURL string) (bool, string, error) {
	// Create registry URL for this specific contract
	registryPath := fmt.Sprintf(nm.paths.ContractRegistry, contractURL)
	registryURL, err := url.Parse(registryPath)
	if err != nil {
		return false, "invalid registry URL", fmt.Errorf("invalid registry URL: %w", err)
	}

	// Query the contract registry entry
	dataAccount, err := nm.querier.QueryDataAccount(ctx, registryURL)
	if err != nil {
		// Contract not found in registry
		return false, "contract not found in registry", nil
	}

	// Check if there are any entries (authorization records)
	if dataAccount.Entry == nil {
		return false, "no authorization records found", nil
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return false, "no authorization records found", nil
	}

	// Get the authorization data
	authData := string(entryData[0])

	// Parse the authorization data
	// Expected format: "status:authorized" or "status:denied"
	// Can also include additional metadata like "authorized_by:some-authority"
	lines := strings.Split(authData, "\n")
	status := ""
	authorizedBy := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") {
			status = strings.TrimPrefix(line, "status:")
		} else if strings.HasPrefix(line, "authorized_by:") {
			authorizedBy = strings.TrimPrefix(line, "authorized_by:")
		}
	}

	switch strings.ToLower(status) {
	case "authorized":
		if authorizedBy != "" {
			return true, fmt.Sprintf("authorized by %s", authorizedBy), nil
		}
		return true, "authorized", nil
	case "denied":
		return false, "explicitly denied", nil
	default:
		return false, "invalid authorization status", nil
	}
}

// GetContractAuthorization retrieves the full authorization details for a contract
func (nm *NamespaceManager) GetContractAuthorization(ctx context.Context, contractURL string) (*ContractAuthorization, error) {
	registryPath := fmt.Sprintf(nm.paths.ContractRegistry, contractURL)
	registryURL, err := url.Parse(registryPath)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	dataAccount, err := nm.querier.QueryDataAccount(ctx, registryURL)
	if err != nil {
		return &ContractAuthorization{
			ContractURL: contractURL,
			Status:      "not_found",
			Reason:      "contract not found in registry",
		}, nil
	}

	if dataAccount.Entry == nil {
		return &ContractAuthorization{
			ContractURL: contractURL,
			Status:      "not_found",
			Reason:      "no authorization records found",
		}, nil
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return &ContractAuthorization{
			ContractURL: contractURL,
			Status:      "not_found",
			Reason:      "no authorization records found",
		}, nil
	}

	// Parse the authorization entry data
	authData := string(entryData[0])

	auth := &ContractAuthorization{
		ContractURL: contractURL,
		RawData:     authData,
	}

	// Parse the authorization data
	lines := strings.Split(authData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") {
			auth.Status = strings.TrimPrefix(line, "status:")
		} else if strings.HasPrefix(line, "authorized_by:") {
			auth.AuthorizedBy = strings.TrimPrefix(line, "authorized_by:")
		} else if strings.HasPrefix(line, "reason:") {
			auth.Reason = strings.TrimPrefix(line, "reason:")
		} else if strings.HasPrefix(line, "expires:") {
			auth.Expires = strings.TrimPrefix(line, "expires:")
		}
	}

	return auth, nil
}

// ContractAuthorization represents the authorization status of a contract
type ContractAuthorization struct {
	ContractURL  string `json:"contract_url"`
	Status       string `json:"status"` // "authorized", "denied", "not_found"
	AuthorizedBy string `json:"authorized_by"`
	Reason       string `json:"reason"`
	Expires      string `json:"expires"`
	RawData      string `json:"raw_data"`
}

// IsAuthorized returns true if the contract is explicitly authorized
func (ca *ContractAuthorization) IsAuthorized() bool {
	return strings.ToLower(ca.Status) == "authorized"
}

// ListReservedNamespaces retrieves all reserved namespaces
func (nm *NamespaceManager) ListReservedNamespaces(ctx context.Context) ([]string, error) {
	reservedURL, err := url.Parse(nm.paths.ReservedNamespaces)
	if err != nil {
		return nil, fmt.Errorf("invalid reserved namespaces URL: %w", err)
	}

	dataAccount, err := nm.querier.QueryDataAccount(ctx, reservedURL)
	if err != nil {
		return []string{}, nil // Return empty list if not found
	}

	if dataAccount.Entry == nil {
		return []string{}, nil
	}

	// Get data from the entry using GetData() method
	entryData := dataAccount.Entry.GetData()
	if len(entryData) == 0 {
		return []string{}, nil
	}

	// Parse the reserved namespaces from the entry data
	reservedNamespaces := strings.Split(string(entryData[0]), "\n")

	// Filter out empty lines and trim whitespace
	var filteredNamespaces []string
	for _, namespace := range reservedNamespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace != "" {
			filteredNamespaces = append(filteredNamespaces, namespace)
		}
	}

	return filteredNamespaces, nil
}
