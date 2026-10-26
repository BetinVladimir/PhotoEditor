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

	return c, nil
}
func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}
