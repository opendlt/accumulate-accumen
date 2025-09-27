package authority

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
)

// Binding represents the binding of an L1 contract to an L0 authority
type Binding struct {
	Contract   *url.URL  `json:"contract"`   // L1 contract URL (acc://<adi>/accumen/<contract>)
	KeyBook    *url.URL  `json:"keyBook"`    // L0 key book URL (acc://<adi>/book)
	KeyPage    *url.URL  `json:"keyPage"`    // L0 key page URL (acc://<adi>/book/<page>)
	PubKeyHash []byte    `json:"pubKeyHash"` // ed25519 sha256-ripemd160 per Accumulate convention
	Perms      []string  `json:"perms"`      // ["writeData","sendTokens","updateAuth"]
	KeyAlias   string    `json:"keyAlias"`   // Keystore alias for the signing key
	CreatedAt  time.Time `json:"createdAt"`  // When this binding was created
}

// PermissionType represents the types of L0 operations that can be permitted
type PermissionType string

const (
	PermWriteData  PermissionType = "writeData"
	PermSendTokens PermissionType = "sendTokens"
	PermUpdateAuth PermissionType = "updateAuth"
)

// AllPermissions returns all available permission types
func AllPermissions() []string {
	return []string{
		string(PermWriteData),
		string(PermSendTokens),
		string(PermUpdateAuth),
	}
}

// IsValidPermission checks if a permission string is valid
func IsValidPermission(perm string) bool {
	switch PermissionType(perm) {
	case PermWriteData, PermSendTokens, PermUpdateAuth:
		return true
	default:
		return false
	}
}

// NewBinding creates a new binding with validation
func NewBinding(contract, keyBook, keyPage *url.URL, pubKeyHash []byte, perms []string, keyAlias string) (*Binding, error) {
	// Validate contract URL
	if err := accutil.ValidateContractAddr(contract); err != nil {
		return nil, fmt.Errorf("invalid contract URL: %w", err)
	}

	// Validate key book URL
	if !accutil.IsKeyBook(keyBook) {
		return nil, fmt.Errorf("invalid key book URL: %s", keyBook)
	}

	// Validate key page URL
	if !accutil.IsKeyPage(keyPage) {
		return nil, fmt.Errorf("invalid key page URL: %s", keyPage)
	}

	// Ensure key page is under the key book
	bookADI, err := accutil.GetADI(keyBook)
	if err != nil {
		return nil, fmt.Errorf("failed to get ADI from key book: %w", err)
	}
	pageADI, err := accutil.GetADI(keyPage)
	if err != nil {
		return nil, fmt.Errorf("failed to get ADI from key page: %w", err)
	}
	if accutil.Canonicalize(bookADI) != accutil.Canonicalize(pageADI) {
		return nil, fmt.Errorf("key page must be under the same ADI as key book")
	}

	// Validate public key hash (should be 20 bytes for sha256-ripemd160)
	if len(pubKeyHash) != 20 {
		return nil, fmt.Errorf("public key hash must be 20 bytes (sha256-ripemd160), got %d", len(pubKeyHash))
	}

	// Validate permissions
	if len(perms) == 0 {
		return nil, fmt.Errorf("at least one permission must be specified")
	}
	for _, perm := range perms {
		if !IsValidPermission(perm) {
			return nil, fmt.Errorf("invalid permission: %s", perm)
		}
	}

	// Validate key alias
	if strings.TrimSpace(keyAlias) == "" {
		return nil, fmt.Errorf("key alias cannot be empty")
	}

	// Remove duplicates from permissions
	uniquePerms := make([]string, 0, len(perms))
	seen := make(map[string]bool)
	for _, perm := range perms {
		if !seen[perm] {
			uniquePerms = append(uniquePerms, perm)
			seen[perm] = true
		}
	}

	return &Binding{
		Contract:   contract,
		KeyBook:    keyBook,
		KeyPage:    keyPage,
		PubKeyHash: pubKeyHash,
		Perms:      uniquePerms,
		KeyAlias:   strings.TrimSpace(keyAlias),
		CreatedAt:  time.Now(),
	}, nil
}

// HasPermission checks if the binding has a specific permission
func (b *Binding) HasPermission(perm PermissionType) bool {
	permStr := string(perm)
	for _, p := range b.Perms {
		if p == permStr {
			return true
		}
	}
	return false
}

// Validate checks if the binding is valid
func (b *Binding) Validate() error {
	if b == nil {
		return fmt.Errorf("binding cannot be nil")
	}

	if b.Contract == nil {
		return fmt.Errorf("contract URL cannot be nil")
	}

	if b.KeyBook == nil {
		return fmt.Errorf("key book URL cannot be nil")
	}

	if b.KeyPage == nil {
		return fmt.Errorf("key page URL cannot be nil")
	}

	if len(b.PubKeyHash) != 20 {
		return fmt.Errorf("public key hash must be 20 bytes, got %d", len(b.PubKeyHash))
	}

	if len(b.Perms) == 0 {
		return fmt.Errorf("permissions cannot be empty")
	}

	if strings.TrimSpace(b.KeyAlias) == "" {
		return fmt.Errorf("key alias cannot be empty")
	}

	// Validate each permission
	for _, perm := range b.Perms {
		if !IsValidPermission(perm) {
			return fmt.Errorf("invalid permission: %s", perm)
		}
	}

	// Validate URLs
	if err := accutil.ValidateContractAddr(b.Contract); err != nil {
		return fmt.Errorf("invalid contract URL: %w", err)
	}

	if !accutil.IsKeyBook(b.KeyBook) {
		return fmt.Errorf("invalid key book URL: %s", b.KeyBook)
	}

	if !accutil.IsKeyPage(b.KeyPage) {
		return fmt.Errorf("invalid key page URL: %s", b.KeyPage)
	}

	return nil
}

// Registry manages authority bindings using the state KV store
type Registry struct {
	store state.KVStore
}

// NewRegistry creates a new authority binding registry
func NewRegistry(store state.KVStore) *Registry {
	return &Registry{
		store: store,
	}
}

// bindingKey generates a storage key for a contract binding
func bindingKey(contractURL *url.URL) string {
	canonical := accutil.Canonicalize(contractURL)
	return fmt.Sprintf("/authority/bind/%s", canonical)
}

// Store saves a binding to the registry
func (r *Registry) Store(binding *Binding) error {
	if err := binding.Validate(); err != nil {
		return fmt.Errorf("invalid binding: %w", err)
	}

	key := bindingKey(binding.Contract)
	data, err := json.Marshal(binding)
	if err != nil {
		return fmt.Errorf("failed to marshal binding: %w", err)
	}

	if err := r.store.Set([]byte(key), data); err != nil {
		return fmt.Errorf("failed to store binding: %w", err)
	}

	return nil
}

// Load retrieves a binding from the registry
func (r *Registry) Load(contractURL *url.URL) (*Binding, error) {
	if contractURL == nil {
		return nil, fmt.Errorf("contract URL cannot be nil")
	}

	key := bindingKey(contractURL)
	data, err := r.store.Get([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("failed to get binding: %w", err)
	}

	if data == nil {
		return nil, fmt.Errorf("binding not found for contract: %s", contractURL)
	}

	var binding Binding
	if err := json.Unmarshal(data, &binding); err != nil {
		return nil, fmt.Errorf("failed to unmarshal binding: %w", err)
	}

	// Validate the loaded binding
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("loaded binding is invalid: %w", err)
	}

	return &binding, nil
}

// Exists checks if a binding exists for a contract
func (r *Registry) Exists(contractURL *url.URL) (bool, error) {
	if contractURL == nil {
		return false, fmt.Errorf("contract URL cannot be nil")
	}

	key := bindingKey(contractURL)
	data, err := r.store.Get([]byte(key))
	if err != nil {
		return false, fmt.Errorf("failed to check binding existence: %w", err)
	}

	return data != nil, nil
}

// Delete removes a binding from the registry
func (r *Registry) Delete(contractURL *url.URL) error {
	if contractURL == nil {
		return fmt.Errorf("contract URL cannot be nil")
	}

	key := bindingKey(contractURL)
	if err := r.store.Delete([]byte(key)); err != nil {
		return fmt.Errorf("failed to delete binding: %w", err)
	}

	return nil
}

// List returns all bindings in the registry
func (r *Registry) List() ([]*Binding, error) {
	prefix := "/authority/bind/"

	iter, err := r.store.Iterator([]byte(prefix))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var bindings []*Binding
	for iter.Valid() {
		key := iter.Key()
		value := iter.Value()

		// Skip if key doesn't have the expected prefix
		if !strings.HasPrefix(string(key), prefix) {
			iter.Next()
			continue
		}

		var binding Binding
		if err := json.Unmarshal(value, &binding); err != nil {
			// Log error but continue with other bindings
			iter.Next()
			continue
		}

		// Validate the binding before adding it to results
		if err := binding.Validate(); err != nil {
			// Log error but continue with other bindings
			iter.Next()
			continue
		}

		bindings = append(bindings, &binding)
		iter.Next()
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return bindings, nil
}

// ListByKeyBook returns all bindings for a specific key book
func (r *Registry) ListByKeyBook(keyBookURL *url.URL) ([]*Binding, error) {
	allBindings, err := r.List()
	if err != nil {
		return nil, err
	}

	canonical := accutil.Canonicalize(keyBookURL)
	var result []*Binding
	for _, binding := range allBindings {
		if accutil.Canonicalize(binding.KeyBook) == canonical {
			result = append(result, binding)
		}
	}

	return result, nil
}

// ListByContract returns all bindings for a specific contract
func (r *Registry) ListByContract(contractURL *url.URL) ([]*Binding, error) {
	// Since each contract can only have one binding, this is equivalent to Load
	binding, err := r.Load(contractURL)
	if err != nil {
		return nil, err
	}

	return []*Binding{binding}, nil
}

// Update modifies an existing binding
func (r *Registry) Update(contractURL *url.URL, updateFn func(*Binding) error) error {
	// Load existing binding
	binding, err := r.Load(contractURL)
	if err != nil {
		return fmt.Errorf("failed to load binding for update: %w", err)
	}

	// Apply the update function
	if err := updateFn(binding); err != nil {
		return fmt.Errorf("update function failed: %w", err)
	}

	// Store the updated binding
	if err := r.Store(binding); err != nil {
		return fmt.Errorf("failed to store updated binding: %w", err)
	}

	return nil
}

// Stats returns statistics about the registry
type Stats struct {
	TotalBindings    int            `json:"totalBindings"`
	PermissionCounts map[string]int `json:"permissionCounts"`
}

// GetStats returns statistics about the registry
func (r *Registry) GetStats() (*Stats, error) {
	bindings, err := r.List()
	if err != nil {
		return nil, fmt.Errorf("failed to get bindings for stats: %w", err)
	}

	stats := &Stats{
		TotalBindings:    len(bindings),
		PermissionCounts: make(map[string]int),
	}

	// Count permissions
	for _, binding := range bindings {
		for _, perm := range binding.Perms {
			stats.PermissionCounts[perm]++
		}
	}

	return stats, nil
}
