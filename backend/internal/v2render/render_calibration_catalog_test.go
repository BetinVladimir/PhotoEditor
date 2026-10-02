package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"strings"
	"syscall"
	"testing"
)

func calibrationManifest(t *testing.T, profiles []RenderCalibration) ([]byte, string) {
	t.Helper()
	raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "profiles": profiles})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:])
}

func TestPinnedRenderCalibrationCatalogOwnsVerifiedProfiles(t *testing.T) {
	original := testRenderCalibration()
	raw, pin := calibrationManifest(t, []RenderCalibration{original})
	catalog, err := NewPinnedRenderCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'x'
	first, err := catalog.Resolve(context.Background(), original.Qualification)
	if err != nil {
		t.Fatal(err)
	}
	first.Samples[0].Provider.VRAMBytes = 1
	next, err := catalog.Resolve(context.Background(), original.Qualification)
	if err != nil || next.Samples[0].Provider.VRAMBytes != 300 {
		t.Fatal("caller mutated installed registry", next, err)
	}
	foreign := original.Qualification
	foreign.DeviceSHA = strings.Repeat("0", 64)
	if _, err := catalog.Resolve(context.Background(), foreign); err == nil {
		t.Fatal("different device admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.Resolve(ctx, original.Qualification); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPinnedRenderCalibrationCatalogRejectsUntrustedManifest(t *testing.T) {
	profile := testRenderCalibration()
	raw, pin := calibrationManifest(t, []RenderCalibration{profile})
	if _, err := NewPinnedRenderCalibrationCatalog(raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong deployment pin accepted")
	}
	spaced := append([]byte{' '}, raw...)
	digest := sha256.Sum256(spaced)
	if _, err := NewPinnedRenderCalibrationCatalog(spaced, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("noncanonical bytes accepted")
	}
	if _, err := NewPinnedRenderCalibrationCatalog(raw, pin); err != nil {
		t.Fatal(err)
	}
	duplicate, duplicatePin := calibrationManifest(t, []RenderCalibration{profile, profile})
	if _, err := NewPinnedRenderCalibrationCatalog(duplicate, duplicatePin); err == nil {
		t.Fatal("ambiguous profile accepted")
	}
	unknown, err := canonical.Marshal(map[string]any{"schema_version": 1, "profiles": []RenderCalibration{profile}, "trust_me": true})
	if err != nil {
		t.Fatal(err)
	}
	digest = sha256.Sum256(unknown)
	if _, err := NewPinnedRenderCalibrationCatalog(unknown, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestLoadPinnedRenderCalibrationRegularBoundedFile(t *testing.T) {
	raw, pin := calibrationManifest(t, []RenderCalibration{testRenderCalibration()})
	directory := t.TempDir()
	path := filepath.Join(directory, "calibration.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadPinnedRenderCalibrationCatalog(path, pin)
	if err != nil || catalog == nil {
		t.Fatal("pinned file load", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRenderCalibrationCatalog(path, pin); err == nil {
		t.Fatal("replaced bytes admitted")
	}
	if err := os.WriteFile(path, make([]byte, 1048577), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRenderCalibrationCatalog(path, pin); err == nil {
		t.Fatal("oversized file admitted")
	}
	for _, invalid := range []string{"relative.json", directory, filepath.Join(directory, "missing")} {
		if _, err := LoadPinnedRenderCalibrationCatalog(invalid, pin); err == nil {
			t.Fatal("invalid path admitted")
		}
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedRenderCalibrationCatalog(fifo, pin); err == nil {
		t.Fatal("FIFO admitted")
	}
}

func TestRenderCalibrationManifestRequiresExplicitMeasurements(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any, map[string]any, map[string]any, map[string]any)
	}{
		{"missing peak", func(root, profile, sample, peak map[string]any) { delete(sample, "provider_peak") }},
		{"null peak", func(root, profile, sample, peak map[string]any) { sample["provider_peak"] = nil }},
		{"missing ram", func(root, profile, sample, peak map[string]any) { delete(peak, "ram_bytes") }},
		{"missing vram", func(root, profile, sample, peak map[string]any) { delete(peak, "vram_bytes") }},
		{"missing scratch", func(root, profile, sample, peak map[string]any) { delete(peak, "scratch_bytes") }},
		{"missing output", func(root, profile, sample, peak map[string]any) { delete(peak, "output_bytes") }},
		{"null vram", func(root, profile, sample, peak map[string]any) { peak["vram_bytes"] = nil }},
		{"case alias", func(root, profile, sample, peak map[string]any) {
			peak["VRAM_BYTES"] = peak["vram_bytes"]
			delete(peak, "vram_bytes")
		}},
		{"duplicate case alias", func(root, profile, sample, peak map[string]any) { peak["VRAM_BYTES"] = 0 }},
		{"profile alias", func(root, profile, sample, peak map[string]any) {
			profile["TILE_SIZE"] = profile["tile_size"]
			delete(profile, "tile_size")
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := calibrationManifest(t, []RenderCalibration{testRenderCalibration()})
			var root map[string]any
			if err := json.Unmarshal(raw, &root); err != nil {
				t.Fatal(err)
			}
			profile := root["profiles"].([]any)[0].(map[string]any)
			sample := profile["samples"].([]any)[0].(map[string]any)
			peak := sample["provider_peak"].(map[string]any)
			test.mutate(root, profile, sample, peak)
			altered, err := canonical.Marshal(root)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(altered)
			if _, err := NewPinnedRenderCalibrationCatalog(altered, hex.EncodeToString(sum[:])); err == nil {
				t.Fatal("absent measurement or alias admitted")
			}
		})
	}
	// Zero overhead is valid only when all four measurements are explicitly given.
	profile := testRenderCalibration()
	profile.Samples[0].Provider = RenderProviderPeak{}
	raw, pin := calibrationManifest(t, []RenderCalibration{profile})
	if _, err := NewPinnedRenderCalibrationCatalog(raw, pin); err != nil {
		t.Fatal("explicit zero measurements rejected", err)
	}
}
