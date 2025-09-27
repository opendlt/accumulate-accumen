package signer

import (
	"crypto/ed25519"
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
)

// MultiSigner combines multiple signers to add multiple signatures to an envelope
type MultiSigner struct {
	Signers []Signer
}

// NewMultiSigner creates a new MultiSigner with the provided signers
func NewMultiSigner(signers ...Signer) *MultiSigner {
	return &MultiSigner{
		Signers: signers,
	}
}

// SignEnvelope adds signatures from all configured signers to the envelope
func (m *MultiSigner) SignEnvelope(envelope *build.SignatureBuilder) error {
	if m == nil {
		return fmt.Errorf("MultiSigner is nil")
	}

	if len(m.Signers) == 0 {
		return fmt.Errorf("no signers configured")
	}

	// Apply signatures from all signers
	for i, signer := range m.Signers {
		if signer == nil {
			return fmt.Errorf("signer at index %d is nil", i)
		}

		if err := signer.SignEnvelope(envelope); err != nil {
			alias := signer.GetKeyAlias()
			if alias == "" {
				alias = fmt.Sprintf("signer_%d", i)
			}
			return fmt.Errorf("failed to sign with signer '%s': %w", alias, err)
		}
	}

	return nil
}

// GetPublicKey returns the public key of the first signer (for interface compatibility)
func (m *MultiSigner) GetPublicKey() (ed25519.PublicKey, error) {
	if m == nil || len(m.Signers) == 0 {
		return nil, fmt.Errorf("no signers available")
	}

	return m.Signers[0].GetPublicKey()
}

// GetKeyAlias returns a combined alias indicating multiple signers
func (m *MultiSigner) GetKeyAlias() string {
	if m == nil || len(m.Signers) == 0 {
		return "empty-multisig"
	}

	if len(m.Signers) == 1 {
		return m.Signers[0].GetKeyAlias()
	}

	// Build combined alias
	var aliases []string
	for _, signer := range m.Signers {
		alias := signer.GetKeyAlias()
		if alias == "" {
			alias = "unnamed"
		}
		aliases = append(aliases, alias)
	}

	return fmt.Sprintf("multisig[%s]", joinStrings(aliases, ","))
}

// GetSigners returns all configured signers
func (m *MultiSigner) GetSigners() []Signer {
	if m == nil {
		return nil
	}
	return m.Signers
}

// AddSigner adds a new signer to the MultiSigner
func (m *MultiSigner) AddSigner(signer Signer) error {
	if m == nil {
		return fmt.Errorf("MultiSigner is nil")
	}
	if signer == nil {
		return fmt.Errorf("signer cannot be nil")
	}

	m.Signers = append(m.Signers, signer)
	return nil
}

// Count returns the number of configured signers
func (m *MultiSigner) Count() int {
	if m == nil {
		return 0
	}
	return len(m.Signers)
}

// joinStrings is a simple string join helper
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
