//go:build !withvdk

package vdkshim

import (
	"context"
	"fmt"
	"net/http"

	"github.com/opendlt/accumulate-accumen/sequencer"
)

// RunWithVDK returns an error indicating VDK support was not compiled in
func RunWithVDK(ctx context.Context, seq *sequencer.Sequencer, httpSrv *http.Server) error {
	return fmt.Errorf("VDK support not available - this binary was built without -tags=withvdk. " +
		"To enable VDK support, rebuild with: go build -tags=withvdk")
}
