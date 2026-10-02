package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/domain"
	"strconv"
)

type Config struct {
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
	CaptureOrigin         string
	PortalOrigin          string
	RegistrationEnabled   bool
	MaxJSONBytes          int64
}

func Load() (Config, error) {
	c := Config{Role: env("PHOTO_ROLE", "api"), Listen: env("HTTP_LISTEN", "127.0.0.1:8080"), DatabaseURL: os.Getenv("DATABASE_URL"), Environment: env("APP_ENV", "development"), AppOrigin: env("APP_ORIGIN", "http://localhost:8080"), PortalOrigin: env("PORTAL_ORIGIN", "http://localhost:3000"), MaxJSONBytes: 1048576}
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
	c.CaptureOrigin = env("CAPTURE_ORIGIN", c.AppOrigin)
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
	for _, raw := range []string{c.AppOrigin, c.PortalOrigin, c.CaptureOrigin} {
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
