package config

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func ingressEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"BILLING_STRIPE_WEBHOOK_ENABLED": "true", "BILLING_STRIPE_ENABLED": "false",
		"BILLING_WEBHOOK_DATABASE_URL": "postgres://fixture:fixture-password@127.0.0.1/fixture",
		"STRIPE_ENVIRONMENT":           "test", "STRIPE_PROVIDER_ACCOUNT_ID": uuid.NewString(),
		"STRIPE_ACCOUNT_ID": "acct_fixture", "STRIPE_API_VERSION": "2026-10-28.endive",
		"STRIPE_WEBHOOK_SECRET_REF": "/run/secrets/webhook", "STRIPE_WEBHOOK_PAYLOAD_KEY_REF": "/run/secrets/payload-key", "STRIPE_WEBHOOK_PAYLOAD_KEY_ID": "stripe-payload-v1",
	} {
		t.Setenv(key, value)
	}
	t.Setenv("STRIPE_SECRET_KEY_REF", "")
	t.Setenv("STRIPE_PORTAL_CONFIG_ID", "")
}
func TestStripeIngressCanBeConfiguredWithoutPurchasesOrAPIKey(t *testing.T) {
	ingressEnvironment(t)
	var ingress StripeIngressConfig
	var purchases StripeConfig
	if err := loadStripeIngressConfig(&ingress); err != nil || !ingress.Enabled {
		t.Fatal("ingress unavailable", err)
	}
	if err := loadStripeConfig(&purchases); err != nil || purchases.Enabled {
		t.Fatal("ingress enabled purchases", err)
	}
}
func TestStripeIngressDisabledIgnoresPrivateReferences(t *testing.T) {
	t.Setenv("BILLING_STRIPE_WEBHOOK_ENABLED", "false")
	t.Setenv("STRIPE_ENVIRONMENT", "live")
	var cfg StripeIngressConfig
	if err := loadStripeIngressConfig(&cfg); err != nil || cfg.Enabled {
		t.Fatal(err)
	}
}
func TestStripeIngressRejectsIncompleteOrUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"BILLING_WEBHOOK_DATABASE_URL", ""}, {"STRIPE_ENVIRONMENT", "live"}, {"STRIPE_PROVIDER_ACCOUNT_ID", "00000000-0000-0000-0000-000000000000"},
		{"STRIPE_ACCOUNT_ID", "acct_"}, {"STRIPE_API_VERSION", ""}, {"STRIPE_API_VERSION", " v1"},
		{"STRIPE_WEBHOOK_SECRET_REF", "relative-secret"}, {"STRIPE_WEBHOOK_PAYLOAD_KEY_REF", ""}, {"STRIPE_WEBHOOK_PAYLOAD_KEY_ID", "bad key id"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			ingressEnvironment(t)
			t.Setenv(tc.key, tc.value)
			var cfg StripeIngressConfig
			err := loadStripeIngressConfig(&cfg)
			if err == nil {
				t.Fatal("unsafe ingress configuration accepted")
			}
			if strings.Contains(err.Error(), "fixture-password") {
				t.Fatal("database secret leaked")
			}
		})
	}
}
