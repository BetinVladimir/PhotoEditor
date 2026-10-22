package v2render

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthoredFinalCalibrationFilePinsActualEnvelope(t *testing.T) {
	raw, pin, q := authoredFinalCalibrationEnvelope(t, nil)
	path := filepath.Join(t.TempDir(), "segment.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadPinnedAuthoredFinalCalibrationCatalog(path, pin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ResolveAuthoredFinal(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPinnedAuthoredFinalCalibrationCatalog(path, ""); err == nil {
		t.Fatal("missing pin accepted")
	}
	if err = os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPinnedAuthoredFinalCalibrationCatalog(path, pin); err == nil {
		t.Fatal("changed file accepted")
	}
}
func TestAuthoredFinalCalibrationFileRejectsInvalidSources(t *testing.T) {
	for _, path := range []string{"segment.json", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		if _, err := LoadPinnedAuthoredFinalCalibrationCatalog(path, ""); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "oversize.json")
	if err := os.WriteFile(path, make([]byte, 1048577), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPinnedAuthoredFinalCalibrationCatalog(path, ""); err == nil {
		t.Fatal("oversize source accepted")
	}
}
