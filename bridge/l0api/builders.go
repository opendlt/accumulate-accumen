package l0api

import (
	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
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