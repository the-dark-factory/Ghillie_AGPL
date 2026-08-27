package credit

// http.go asks the FACADE. It is the only thing in this package that can
// produce a Decision, and it produces one by going and asking — never by
// consulting anything held on this machine.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Bearer supplies the Authorization header for the outward request. It is the
// same attestation the poll uses; asking about credit is not a special door.
type Bearer interface {
	Authorization(ctx context.Context) (string, error)
}

// HTTPAuthority is the facade's credit decision, reached over the wire.
type HTTPAuthority struct {
	base   string
	clawID string
	auth   Bearer
	client *http.Client
}

// NewHTTPAuthority builds an authority against a facade base URL.
func NewHTTPAuthority(base, clawID string, auth Bearer, client *http.Client) *HTTPAuthority {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPAuthority{
		base:   strings.TrimRight(base, "/"),
		clawID: clawID,
		auth:   auth,
		client: client,
	}
}

// Decide asks the factory whether this claw may perform this act now.
//
// ⚠ EVERY FAILURE PATH RETURNS AN ERROR AND NEVER A PERMISSIVE DEFAULT. A
// facade that cannot be reached has not said yes. An unparseable answer is not
// a yes. The only yes is a well-formed Sufficient:true from the facade.
func (h *HTTPAuthority) Decide(ctx context.Context, act string) (Decision, error) {
	url := h.base + creditPath(h.clawID, act)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Decision{}, fmt.Errorf("build credit request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if h.auth != nil {
		value, aerr := h.auth.Authorization(ctx)
		if aerr != nil {
			return Decision{}, fmt.Errorf("attest before asking about credit: %w", aerr)
		}
		req.Header.Set("Authorization", value)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("ask %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			// Nothing useful follows from a close failure on a read body, and
			// it must not turn a clear answer into an unclear one.
			return
		}
	}()

	if resp.StatusCode == http.StatusPaymentRequired {
		// An explicit no, with the facade's own words.
		var d Decision
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&d); err != nil {
			return Decision{Sufficient: false, Note: "the factory declined and gave no reason this terminal could read"}, nil
		}
		d.Sufficient = false
		return d, nil
	}
	if resp.StatusCode != http.StatusOK {
		detail, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if rerr != nil {
			return Decision{}, fmt.Errorf("credit: HTTP %d (and reading the body failed: %w)", resp.StatusCode, rerr)
		}
		return Decision{}, fmt.Errorf("credit: HTTP %d: %s", resp.StatusCode, detail)
	}

	var d Decision
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&d); err != nil {
		return Decision{}, fmt.Errorf("decode credit decision: %w", err)
	}
	return d, nil
}

// creditPath is the outward GET. It duplicates protocol.CreditPath rather than
// importing it, so that this package stays free of the poll's wire shapes — the
// credit question is not part of the instruction protocol and should not become
// entangled with it.
func creditPath(clawID, act string) string {
	return "/claws/" + clawID + "/credit?act=" + act
}
