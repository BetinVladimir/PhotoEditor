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
