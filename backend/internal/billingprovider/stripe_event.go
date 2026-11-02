package billingprovider

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v87"
)

var ErrStripeEventInvalid = errors.New("BILLING_STRIPE_EVENT_INVALID")

// StripeEventParser verifies snapshot events for one configured Sandbox endpoint.
// It does not enable purchases or perform provider API calls.
type StripeEventParser struct{ accountID, apiVersion string }

func NewStripeEventParser(accountID, apiVersion string) (*StripeEventParser, error) {
	if !strings.HasPrefix(accountID, "acct_") || len(accountID) <= 5 || strings.TrimSpace(apiVersion) == "" {
		return nil, ErrStripeEventInvalid
	}
	return &StripeEventParser{accountID: accountID, apiVersion: apiVersion}, nil
}

func (p *StripeEventParser) ParseEvent(payload []byte, signature, secret string) (Event, error) {
	if p == nil || p.accountID == "" || p.apiVersion == "" {
		return Event{}, ErrStripeEventInvalid
	}
	// Validate original bytes before decoding; use the SDK's five-minute window.
	if err := stripe.ValidatePayload(payload, signature, secret, stripe.WithTolerance(5*time.Minute)); err != nil {
		return Event{}, ErrStripeEventInvalid
	}
	// The SDK rejects old timestamps. Also reject signed timestamps far in the future.
	var signedAt int64
	for _, part := range strings.Split(signature, ",") {
		if strings.HasPrefix(part, "t=") {
			signedAt, _ = strconv.ParseInt(strings.TrimPrefix(part, "t="), 10, 64)
		}
	}
	if time.Unix(signedAt, 0).After(time.Now().Add(5 * time.Minute)) {
		return Event{}, ErrStripeEventInvalid
	}
	var envelope struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Type    string `json:"type"`
		Account string `json:"account"`
		Version string `json:"api_version"`
		Live    *bool  `json:"livemode"`
		Created int64  `json:"created"`
		Data    struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	// Decode only the versioned envelope; workers decode their explicitly pinned payload paths.
	if json.Unmarshal(payload, &envelope) != nil || envelope.Object != "event" || envelope.ID == "" || envelope.Type == "" ||
		envelope.Live == nil || *envelope.Live || envelope.Version != p.apiVersion || envelope.Created <= 0 ||
		len(envelope.Data.Object) == 0 || envelope.Data.Object[0] != '{' ||
		(envelope.Account != "" && envelope.Account != p.accountID) {
		return Event{}, ErrStripeEventInvalid
	}
	var object struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(envelope.Data.Object, &object) != nil || object.ID == "" {
		return Event{}, ErrStripeEventInvalid
	}
	// Direct account events omit account; the endpoint signing secret binds them to this account.
	return Event{ID: envelope.ID, Type: envelope.Type, AccountID: p.accountID,
		CreatedAt: time.Unix(envelope.Created, 0).UTC(), RawPayload: append([]byte(nil), payload...)}, nil
}
