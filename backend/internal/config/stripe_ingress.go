package config

import (
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// StripeIngressConfig enables signature verification and durable recording only.
// It never enables Checkout, provider API calls or entitlement fulfillment.
type StripeIngressConfig struct {
	Enabled                                       bool
	DatabaseURL                                   string `json:"-"`
	ProviderAccountID                             uuid.UUID
	AccountID, APIVersion                         string
	WebhookSecretRef, PayloadKeyRef, PayloadKeyID string
}

func loadStripeIngressConfig(out *StripeIngressConfig) error {
	if os.Getenv("BILLING_STRIPE_WEBHOOK_ENABLED") != "true" {
		return nil
	}
	out.Enabled = true
	required := []struct {
		name   string
		target *string
	}{
		{"BILLING_WEBHOOK_DATABASE_URL", &out.DatabaseURL},
		{"STRIPE_ACCOUNT_ID", &out.AccountID},
		{"STRIPE_API_VERSION", &out.APIVersion},
		{"STRIPE_WEBHOOK_SECRET_REF", &out.WebhookSecretRef},
		{"STRIPE_WEBHOOK_PAYLOAD_KEY_REF", &out.PayloadKeyRef},
		{"STRIPE_WEBHOOK_PAYLOAD_KEY_ID", &out.PayloadKeyID},
	}
	for _, field := range required {
		*field.target = os.Getenv(field.name)
		if *field.target == "" {
			return fmt.Errorf("BILLING_STRIPE_WEBHOOK_ENABLED=true requires %s", field.name)
		}
	}
	if os.Getenv("STRIPE_ENVIRONMENT") != "test" {
		return fmt.Errorf("STRIPE_ENVIRONMENT must be test for webhook ingress")
	}
	rawID := os.Getenv("STRIPE_PROVIDER_ACCOUNT_ID")
	id, err := uuid.Parse(rawID)
	if err != nil || id == uuid.Nil || id.String() != rawID {
		return fmt.Errorf("STRIPE_PROVIDER_ACCOUNT_ID must be a canonical nonzero UUID")
	}
	out.ProviderAccountID = id
	if !regexp.MustCompile(`^acct_[A-Za-z0-9]+$`).MatchString(out.AccountID) {
		return fmt.Errorf("STRIPE_ACCOUNT_ID is invalid")
	}
	if len(out.APIVersion) > 80 || strings.TrimSpace(out.APIVersion) != out.APIVersion {
		return fmt.Errorf("STRIPE_API_VERSION is invalid")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(out.PayloadKeyID) {
		return fmt.Errorf("STRIPE_WEBHOOK_PAYLOAD_KEY_ID is invalid")
	}
	if !filepath.IsAbs(out.WebhookSecretRef) || !filepath.IsAbs(out.PayloadKeyRef) {
		return fmt.Errorf("Stripe webhook secret references must be absolute paths")
	}
	return nil
}
