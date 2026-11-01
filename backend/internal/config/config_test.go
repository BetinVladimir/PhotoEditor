package config

import (
	"strings"
	"testing"
)

func TestOriginConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local/test")
	t.Setenv("APP_ENV", "production")
	t.Setenv("PORTAL_ORIGIN", "https://portal.example.test")
	t.Setenv("API_PUBLIC_ORIGIN", "https://api.example.test")
	for _, origin := range []string{"http://app.example.test", "https://user@app.example.test", "https://app.example.test/path", "https://app.example.test?query=x", "https://app.example.test#fragment"} {
		t.Setenv("APP_ORIGIN", origin)
		if _, err := Load(); err == nil {
			t.Fatal("unsafe origin", origin)
		}
	}
	t.Setenv("APP_ORIGIN", "https://app.example.test")
	t.Setenv("CAPTURE_ORIGIN", "http://capture.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("unsafe capture origin")
	}
	t.Setenv("CAPTURE_ORIGIN", "https://capture.example.test")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("API_PUBLIC_ORIGIN", "http://api.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("unsafe API public origin")
	}
	t.Setenv("API_PUBLIC_ORIGIN", "")
	if _, err := Load(); err == nil {
		t.Fatal("production API accepted missing public origin")
	}
	t.Setenv("API_PUBLIC_ORIGIN", "https://api.example.test")
	t.Setenv("APP_ENV", "prod")
	if _, err := Load(); err == nil {
		t.Fatal("unknown environment")
	}
}

func TestStorageBootstrapDoesNotRequireDatabase(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "http://localhost:8080")
	t.Setenv("PORTAL_ORIGIN", "http://localhost:3000")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PHOTO_ROLE", "bootstrap-storage")
	if _, err := Load(); err != nil {
		t.Fatal("storage-only bootstrap required DB", err)
	}
	t.Setenv("PHOTO_ROLE", "maintenance")
	if _, err := Load(); err == nil {
		t.Fatal("maintenance accepted missing DB")
	}
}

func TestRenderCalibrationConfigurationPins(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local/test")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "http://localhost:8080")
	t.Setenv("PORTAL_ORIGIN", "http://localhost:3000")
	t.Setenv("CAPTURE_ORIGIN", "http://localhost:8080")
	t.Setenv("PHOTO_RENDER_CALIBRATION_FILE", "")
	t.Setenv("PHOTO_RENDER_CALIBRATION_SHA256", "")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"/profiles/catalog.json", ""}, {"", strings.Repeat("a", 64)}, {"relative.json", strings.Repeat("a", 64)}, {"/profiles/catalog.json", strings.Repeat("A", 64)}} {
		t.Setenv("PHOTO_RENDER_CALIBRATION_FILE", pair[0])
		t.Setenv("PHOTO_RENDER_CALIBRATION_SHA256", pair[1])
		if _, err := Load(); err == nil {
			t.Fatal("invalid calibration config accepted", pair[0])
		}
	}
	t.Setenv("PHOTO_RENDER_CALIBRATION_FILE", "/profiles/catalog.json")
	t.Setenv("PHOTO_RENDER_CALIBRATION_SHA256", strings.Repeat("a", 64))
	cfg, err := Load()
	if err != nil || cfg.RenderCalibrationFile != "/profiles/catalog.json" || cfg.RenderCalibrationSHA != strings.Repeat("a", 64) {
		t.Fatal("valid pinned configuration rejected", err)
	}
}

// setDevBase sets a minimal valid non-Stripe development environment.
func setDevBase(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://local/test")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "http://localhost:8080")
	t.Setenv("PORTAL_ORIGIN", "http://localhost:3000")
	t.Setenv("PHOTO_RENDER_CALIBRATION_FILE", "")
	t.Setenv("PHOTO_RENDER_CALIBRATION_SHA256", "")
	t.Setenv("PHOTO_REGION_CALIBRATION_FILE", "")
	t.Setenv("PHOTO_REGION_CALIBRATION_SHA256", "")
	t.Setenv("PHOTO_REGION_EVIDENCE_SHA256", "")
}

func setStripeEnabled(t *testing.T) {
	t.Helper()
	t.Setenv("BILLING_STRIPE_ENABLED", "true")
	t.Setenv("STRIPE_SECRET_KEY_REF", "/run/secrets/stripe_secret_key")
	t.Setenv("STRIPE_WEBHOOK_SECRET_REF", "/run/secrets/stripe_webhook_secret")
	t.Setenv("STRIPE_ACCOUNT_ID", "acct_test_example")
	t.Setenv("STRIPE_ENVIRONMENT", "test")
	t.Setenv("STRIPE_API_VERSION", "2024-04-10")
	t.Setenv("STRIPE_PORTAL_CONFIG_ID", "bpc_test_example")
}

// A. BILLING_STRIPE_ENABLED absent or not "true" → no errors.
func TestStripeConfig_DisabledNoErrors(t *testing.T) {
	setDevBase(t)
	t.Setenv("BILLING_STRIPE_ENABLED", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("stripe disabled should not produce errors: %v", err)
	}
	if cfg.Stripe.Enabled {
		t.Error("Stripe.Enabled should be false when env var absent")
	}
}

// A cont: set to "false" → still no errors.
func TestStripeConfig_ExplicitFalseNoErrors(t *testing.T) {
	setDevBase(t)
	t.Setenv("BILLING_STRIPE_ENABLED", "false")
	_, err := Load()
	if err != nil {
		t.Fatalf("BILLING_STRIPE_ENABLED=false should not produce errors: %v", err)
	}
}

// B. true without any required variable → error (credentials must not appear in message).
func TestStripeConfig_EnabledMissingAll(t *testing.T) {
	setDevBase(t)
	t.Setenv("BILLING_STRIPE_ENABLED", "true")
	t.Setenv("STRIPE_SECRET_KEY_REF", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET_REF", "")
	t.Setenv("STRIPE_ACCOUNT_ID", "")
	t.Setenv("STRIPE_ENVIRONMENT", "")
	t.Setenv("STRIPE_API_VERSION", "")
	t.Setenv("STRIPE_PORTAL_CONFIG_ID", "")
	_, err := Load()
	if err == nil {
		t.Fatal("enabled Stripe without any variables should fail")
	}
	for _, secret := range []string{"sk_", "whsec_", "rk_"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error must not contain secret value prefix %q: %s", secret, err.Error())
		}
	}
}

// B. Each required variable missing individually → error.
func TestStripeConfig_EnabledMissingEach(t *testing.T) {
	setDevBase(t)
	vars := []string{
		"STRIPE_SECRET_KEY_REF",
		"STRIPE_WEBHOOK_SECRET_REF",
		"STRIPE_ACCOUNT_ID",
		"STRIPE_ENVIRONMENT",
		"STRIPE_API_VERSION",
		"STRIPE_PORTAL_CONFIG_ID",
	}
	for _, v := range vars {
		setStripeEnabled(t)
		t.Setenv(v, "")
		_, err := Load()
		if err == nil {
			t.Errorf("missing %s should produce error", v)
		}
	}
}

// C. STRIPE_ENVIRONMENT != "test" → error.
func TestStripeConfig_LiveEnvironmentRejected(t *testing.T) {
	setDevBase(t)
	setStripeEnabled(t)
	for _, env := range []string{"live", "production", "prod", "LIVE", ""} {
		if env == "" {
			continue // covered by TestStripeConfig_EnabledMissingEach
		}
		t.Setenv("STRIPE_ENVIRONMENT", env)
		_, err := Load()
		if err == nil {
			t.Errorf("STRIPE_ENVIRONMENT=%q should be rejected (R6)", env)
		}
	}
}

// D. Relative path in *_REF → error.
func TestStripeConfig_RelativeRefRejected(t *testing.T) {
	setDevBase(t)
	setStripeEnabled(t)
	t.Setenv("STRIPE_SECRET_KEY_REF", "relative/path/secret")
	_, err := Load()
	if err == nil {
		t.Error("relative STRIPE_SECRET_KEY_REF should be rejected")
	}

	setStripeEnabled(t)
	t.Setenv("STRIPE_WEBHOOK_SECRET_REF", "relative/webhook")
	_, err = Load()
	if err == nil {
		t.Error("relative STRIPE_WEBHOOK_SECRET_REF should be rejected")
	}
}

// E. Full valid Stripe config is accepted.
func TestStripeConfig_ValidEnabled(t *testing.T) {
	setDevBase(t)
	setStripeEnabled(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("valid Stripe config rejected: %v", err)
	}
	if !cfg.Stripe.Enabled {
		t.Error("Stripe.Enabled should be true")
	}
	if cfg.Stripe.Environment != "test" {
		t.Errorf("expected Environment=test, got %q", cfg.Stripe.Environment)
	}
	if cfg.Stripe.SecretKeyRef != "/run/secrets/stripe_secret_key" {
		t.Errorf("SecretKeyRef not loaded: %q", cfg.Stripe.SecretKeyRef)
	}
	if cfg.Stripe.WebhookSecretRef != "/run/secrets/stripe_webhook_secret" {
		t.Errorf("WebhookSecretRef not loaded: %q", cfg.Stripe.WebhookSecretRef)
	}
}

func TestRegionCalibrationConfigurationRequiresCompletePins(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local/test")
	t.Setenv("APP_ENV", "development")
	t.Setenv("PHOTO_RENDER_CALIBRATION_FILE", "")
	t.Setenv("PHOTO_RENDER_CALIBRATION_SHA256", "")
	for _, values := range [][3]string{{"", "", ""}, {"/profiles/region.json", "", ""}, {"", "a", "b"}, {"relative.json", strings.Repeat("a", 64), strings.Repeat("b", 64)}, {"/profiles/region.json", strings.Repeat("a", 64), strings.Repeat("B", 64)}, {"/profiles/region.json", strings.Repeat("a", 64), strings.Repeat("b", 64)}} {
		t.Setenv("PHOTO_REGION_CALIBRATION_FILE", values[0])
		t.Setenv("PHOTO_REGION_CALIBRATION_SHA256", values[1])
		t.Setenv("PHOTO_REGION_EVIDENCE_SHA256", values[2])
		cfg, err := Load()
		valid := values[0] == "" && values[1] == "" && values[2] == "" || values[0] == "/profiles/region.json" && values[1] == strings.Repeat("a", 64) && values[2] == strings.Repeat("b", 64)
		if (err == nil) != valid {
			t.Fatal("Region config triple", values, err)
		}
		if valid && cfg.RegionCalibrationFile != values[0] {
			t.Fatal("lost Region config")
		}
	}
}
