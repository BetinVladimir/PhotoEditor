package billing

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/billingprovider"
	"photoeditor/backend/internal/domain"
)

// StripeWebhookReceiver acknowledges only a successfully committed inbox record.
// It performs no entitlement or payment mutation.
type StripeWebhookReceiver struct {
	Ingress           *billingprovider.WebhookIngress
	ProviderAccountID uuid.UUID
	ExternalAccountID string
}

func (s StripeWebhookReceiver) ReceiveStripeWebhook(ctx context.Context, account uuid.UUID, raw []byte, signature string) (WebhookReceived, error) {
	if err := ctx.Err(); err != nil {
		return WebhookReceived{}, err
	}
	if account == uuid.Nil || account != s.ProviderAccountID {
		return WebhookReceived{}, domain.NewError("NOT_FOUND", 404)
	}
	if s.Ingress == nil || s.ExternalAccountID == "" {
		return WebhookReceived{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	_, err := s.Ingress.IngestRaw(ctx, s.ExternalAccountID, raw, signature)
	if err != nil {
		if ctx.Err() != nil {
			return WebhookReceived{}, ctx.Err()
		}
		if errors.Is(err, billingprovider.ErrStripeEventInvalid) {
			return WebhookReceived{}, domain.NewError("VALIDATION_FAILED", 400)
		}
		if errors.Is(err, billingprovider.ErrWebhookPayloadConflict) {
			return WebhookReceived{}, domain.NewError("BILLING_STATE_CONFLICT", 409)
		}
		return WebhookReceived{}, domain.NewError("SERVICE_UNAVAILABLE", 503)
	}
	return WebhookReceived{Received: true}, nil
}
