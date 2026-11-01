package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/domain"
	"strconv"
	"strings"
)

// StripeConfig holds Stripe integration settings. Only active when Enabled is true.
// SecretKeyRef and WebhookSecretRef are absolute filesystem paths to files containing
// the actual credentials — the credentials themselves are never stored here.
type StripeConfig struct {
	Enabled          bool
	SecretKeyRef     string // absolute path; file contains sk_test_... value
	WebhookSecretRef string // absolute path; file contains whsec_... value
	AccountID        string // acct_... Stripe account identifier
	Environment      string // must be "test" (R6: no live Stripe)
	APIVersion       string // e.g. "2024-04-10"
	PortalConfigID   string // bpc_... Stripe portal configuration
}

type Config struct {
	// TrustedProxyCIDRs lists reverse proxies (Caddy) whose X-Forwarded-For is trusted for client IP.
	TrustedProxyCIDRs     []string
	RegionCalibrationFile string
	RegionCalibrationSHA  string
	RegionEvidenceSHA     string
	RenderCalibrationFile string
	RenderCalibrationSHA  string
	AMQPURL               string
	AuthDatabaseURL       string
	AuthKeysFile          string
	JWTIssuer             string
	JWTAudience           string
	Role                  string
	Listen                string
	DatabaseURL           string
	Environment           string
	AppOrigin             string
	APIPublicOrigin       string
	CaptureOrigin         string
	PortalOrigin          string
	RegistrationEnabled   bool
	MaxJSONBytes          int64
	Stripe                StripeConfig
}

func Load() (Config, error) {
	c := Config{Role: env("PHOTO_ROLE", "api"), Listen: env("HTTP_LISTEN", "127.0.0.1:8080"), DatabaseURL: os.Getenv("DATABASE_URL"), Environment: env("APP_ENV", "development"), AppOrigin: env("APP_ORIGIN", "http://localhost:8080"), PortalOrigin: env("PORTAL_ORIGIN", "http://localhost:3000"), MaxJSONBytes: 1048576}
	for _, part := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if part = strings.TrimSpace(part); part != "" {
			c.TrustedProxyCIDRs = append(c.TrustedProxyCIDRs, part)
		}
	}
	c.RenderCalibrationFile = os.Getenv("PHOTO_RENDER_CALIBRATION_FILE")
	c.RenderCalibrationSHA = os.Getenv("PHOTO_RENDER_CALIBRATION_SHA256")
	if (c.RenderCalibrationFile == "") != (c.RenderCalibrationSHA == "") {
		return c, fmt.Errorf("render calibration file and SHA256 must be configured together")
	}
	if c.RenderCalibrationFile != "" {
		if !filepath.IsAbs(c.RenderCalibrationFile) {
			return c, fmt.Errorf("render calibration file must be absolute")
		}
		if _, err := domain.ParseDigest(c.RenderCalibrationSHA); err != nil {
			return c, fmt.Errorf("render calibration SHA256 invalid")
		}
	}
	c.RegionCalibrationFile = os.Getenv("PHOTO_REGION_CALIBRATION_FILE")
	c.RegionCalibrationSHA = os.Getenv("PHOTO_REGION_CALIBRATION_SHA256")
	c.RegionEvidenceSHA = os.Getenv("PHOTO_REGION_EVIDENCE_SHA256")
	if c.RegionCalibrationFile != "" || c.RegionCalibrationSHA != "" || c.RegionEvidenceSHA != "" {
		if !filepath.IsAbs(c.RegionCalibrationFile) {
			return c, fmt.Errorf("region calibration file must be absolute")
		}
		if _, err := domain.ParseDigest(c.RegionCalibrationSHA); err != nil {
			return c, fmt.Errorf("region calibration SHA256 invalid")
		}
		if _, err := domain.ParseDigest(c.RegionEvidenceSHA); err != nil {
			return c, fmt.Errorf("region evidence SHA256 invalid")
		}
	}
	c.CaptureOrigin = env("CAPTURE_ORIGIN", c.AppOrigin)
	c.APIPublicOrigin = os.Getenv("API_PUBLIC_ORIGIN")
	if c.APIPublicOrigin == "" {
		if c.Environment == "production" && c.Role == "api" {
			return c, fmt.Errorf("API_PUBLIC_ORIGIN is required for production API")
		}
		c.APIPublicOrigin = "http://localhost:8080"
	}
	c.AMQPURL = os.Getenv("AMQP_URL")
	c.AuthDatabaseURL = os.Getenv("AUTH_DATABASE_URL")
	c.AuthKeysFile = os.Getenv("AUTH_KEYS_FILE")
	c.JWTIssuer = env("JWT_ISSUER", "photoeditor")
	c.JWTAudience = env("JWT_AUDIENCE", "photo-api")
	var err error
	c.RegistrationEnabled, err = strconv.ParseBool(env("REGISTRATION_ENABLED", "false"))
	if err != nil {
		return c, fmt.Errorf("REGISTRATION_ENABLED must be boolean")
	}
	if c.DatabaseURL == "" && c.Role != "bootstrap-queue" && c.Role != "bootstrap-storage" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.Environment != "development" && c.Environment != "production" {
		return c, fmt.Errorf("APP_ENV must be development or production")
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, e := net.ParseCIDR(cidr); e != nil {
			return c, fmt.Errorf("TRUSTED_PROXY_CIDRS must be a comma-separated CIDR list")
		}
	}
	for _, raw := range []string{c.AppOrigin, c.PortalOrigin, c.CaptureOrigin, c.APIPublicOrigin} {
		u, e := url.Parse(raw)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return c, fmt.Errorf("origins must be exact HTTP(S) origins without path")
		}
		if c.Environment == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("production origins require HTTPS")
		}
		if u.Scheme == "http" {
			ip := net.ParseIP(u.Hostname())
			if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
				return c, fmt.Errorf("HTTP development origins must use loopback")
			}
		}
	}
	if _, _, err = net.SplitHostPort(c.Listen); err != nil {
		return c, fmt.Errorf("HTTP_LISTEN must be host:port")
	}

	if err = loadStripeConfig(&c.Stripe); err != nil {
		return c, err
	}

	return c, nil
}

func loadStripeConfig(s *StripeConfig) error {
	enabled := os.Getenv("BILLING_STRIPE_ENABLED")
	if enabled != "true" {
		return nil
	}
	s.Enabled = true
	s.SecretKeyRef = os.Getenv("STRIPE_SECRET_KEY_REF")
	s.WebhookSecretRef = os.Getenv("STRIPE_WEBHOOK_SECRET_REF")
	s.AccountID = os.Getenv("STRIPE_ACCOUNT_ID")
	s.Environment = os.Getenv("STRIPE_ENVIRONMENT")
	s.APIVersion = os.Getenv("STRIPE_API_VERSION")
	s.PortalConfigID = os.Getenv("STRIPE_PORTAL_CONFIG_ID")

	var missing []string
	if s.SecretKeyRef == "" {
		missing = append(missing, "STRIPE_SECRET_KEY_REF")
	}
	if s.WebhookSecretRef == "" {
		missing = append(missing, "STRIPE_WEBHOOK_SECRET_REF")
	}
	if s.AccountID == "" {
		missing = append(missing, "STRIPE_ACCOUNT_ID")
	}
	if s.Environment == "" {
		missing = append(missing, "STRIPE_ENVIRONMENT")
	}
	if s.APIVersion == "" {
		missing = append(missing, "STRIPE_API_VERSION")
	}
	if s.PortalConfigID == "" {
		missing = append(missing, "STRIPE_PORTAL_CONFIG_ID")
	}
	if len(missing) > 0 {
		return fmt.Errorf("BILLING_STRIPE_ENABLED=true requires: %s", strings.Join(missing, ", "))
	}

	// R6: only test/sandbox Stripe environment allowed
	if s.Environment != "test" {
		return fmt.Errorf("STRIPE_ENVIRONMENT must be \"test\"; live mode is not permitted")
	}

	// *_REF values are filesystem paths — they must be absolute so the server
	// can open them unambiguously regardless of working directory.
	if !filepath.IsAbs(s.SecretKeyRef) {
		return fmt.Errorf("STRIPE_SECRET_KEY_REF must be an absolute path")
	}
	if !filepath.IsAbs(s.WebhookSecretRef) {
		return fmt.Errorf("STRIPE_WEBHOOK_SECRET_REF must be an absolute path")
	}

	return nil
}
func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}
