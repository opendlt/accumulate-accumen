package l0api

import (
	"encoding/base64"
	"fmt"

	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"

	"github.com/opendlt/accumulate-accumen/internal/accutil"
)

// BuildWriteData creates a write data transaction envelope with proper header and memo
func BuildWriteData(to *url.URL, data []byte, memo string, meta any) *build.EnvelopeBuilder {
	env := build.Transaction().
		For(to).
		Body(&protocol.WriteData{
			Entry: &protocol.DataEntry{
				Data: data,
			},
		})

	// Set memo if provided
	if memo != "" {
		env = accutil.WithMemo(env, memo)
	}

	// Set metadata if provided
	if meta != nil {
		env = accutil.WithMetadataJSON(env, meta)
	}

	return env
}

// BuildSendTokens creates a send tokens transaction envelope with proper header and memo
func BuildSendTokens(fromAcct *url.URL, toAcct *url.URL, amount string, token *url.URL, memo string) *build.EnvelopeBuilder {
	env := build.Transaction().
		For(fromAcct).
		Body(&protocol.SendTokens{
			To: []*protocol.TokenRecipient{
				{
					Url:    toAcct,
					Amount: protocol.ParseBigInt(amount),
				},
			},
		})

	// Set token URL if different from default
	if token != nil {
		body := env.GetBody().(*protocol.SendTokens)
		body.To[0].Token = token
	}

	// Set memo if provided
	if memo != "" {
		env = accutil.WithMemo(env, memo)
	}

	return env
}

// BuildAddCredits creates an add credits transaction envelope with proper header and memo
func BuildAddCredits(page *url.URL, fromToken *url.URL, amountCredits uint64, memo string) *build.EnvelopeBuilder {
	env := build.Transaction().
		For(page).
		Body(&protocol.AddCredits{
			Recipient: page,
			Amount:    protocol.ParseBigInt(protocol.FormatBigInt(protocol.NewBigInt(amountCredits))),
			Oracle:    fromToken,
		})

	// Set memo if provided
	if memo != "" {
		env = accutil.WithMemo(env, memo)
	}

	return env
}

// EncodeEnvelopeBase64 encodes a completed envelope to base64 string
func EncodeEnvelopeBase64(envelope *messaging.Envelope) (string, error) {
	if envelope == nil {
		return "", fmt.Errorf("envelope cannot be nil")
	}

	// Serialize the envelope to bytes
	data, err := envelope.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("failed to marshal envelope: %w", err)
	}

	// Encode to base64
	return base64.StdEncoding.EncodeToString(data), nil
}

// DecodeEnvelopeBase64 decodes a base64 string to an envelope
func DecodeEnvelopeBase64(encodedEnvelope string) (*messaging.Envelope, error) {
	if encodedEnvelope == "" {
		return nil, fmt.Errorf("encoded envelope cannot be empty")
	}

	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(encodedEnvelope)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// Unmarshal the envelope
	var envelope messaging.Envelope
	if err := envelope.UnmarshalBinary(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal envelope: %w", err)
	}

	return &envelope, nil
}

// EncodeEnvelopeBuilderBase64 encodes an envelope builder to base64 string by completing it first
func EncodeEnvelopeBuilderBase64(builder *build.EnvelopeBuilder) (string, error) {
	if builder == nil {
		return "", fmt.Errorf("envelope builder cannot be nil")
	}

	// Complete the envelope
	envelope, err := builder.Done()
	if err != nil {
		return "", fmt.Errorf("failed to complete envelope: %w", err)
	}

	// Encode to base64
	return EncodeEnvelopeBase64(envelope)
}

// DecodeToEnvelopeBuilder decodes a base64 string to an envelope and wraps it in a builder for further modifications
func DecodeToEnvelopeBuilder(encodedEnvelope string) (*build.EnvelopeBuilder, error) {
	envelope, err := DecodeEnvelopeBase64(encodedEnvelope)
	if err != nil {
		return nil, err
	}

	// Create a new builder from the existing envelope
	// Note: This requires the envelope to be reconstructed as a builder
	// The exact implementation depends on the build package API
	return build.Transaction().For(envelope), nil
}

// ValidateEnvelopeBase64 validates that a base64 string represents a valid envelope
func ValidateEnvelopeBase64(encodedEnvelope string) error {
	_, err := DecodeEnvelopeBase64(encodedEnvelope)
	return err
}