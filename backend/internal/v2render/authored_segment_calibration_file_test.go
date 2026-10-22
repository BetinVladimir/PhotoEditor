package v2render

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthoredSegmentCalibrationFilePinsActualEnvelope(t *testing.T) {
	raw, pin, q := segmentCalibrationEnvelope(t, nil)
	path := filepath.Join(t.TempDir(), "segment.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadPinnedAuthoredSegmentCalibrationCatalog(path, pin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ResolveAuthoredSegment(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPinnedAuthoredSegmentCalibrationCatalog(path, ""); err == nil {
		t.Fatal("missing pin accepted")
	}
	if err = os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPinnedAuthoredSegmentCalibrationCatalog(path, pin); err == nil {
		t.Fatal("changed file accepted")
	}
}
func TestAuthoredSegmentCalibrationFileRejectsInvalidSources(t *testing.T) {
	for _, path := range []string{"segment.json", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		if _, err := LoadPinnedAuthoredSegmentCalibrationCatalog(path, ""); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "oversize.json")
	if err := os.WriteFile(path, make([]byte, 1048577), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedAuthoredSegmentCalibrationCatalog(path, ""); err == nil {
		t.Fatal("oversize source accepted")
	}
}
