package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/semanticreview"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestSemanticExposureCatalogExactImmutableFile(t *testing.T) {
	sha := strings.Repeat("a", 64)
	pin := semanticreview.ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Backend: "cpu", ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha}
	config := semanticreview.ExposurePreparationCatalog{SchemaVersion: 1, Tenants: []semanticreview.ExposureTenantPreparation{{TenantID: uuid.New(), Review: semanticreview.ReviewPreparationConfig{SchemaVersion: 1, ModelSHA: sha, DatasetSHA: sha, DocumentBudgetBytes: 64 << 20}, Execution: semanticreview.ExposureDeploymentBudget{Qualification: pin, OwnerCosts: semanticreview.ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}, Limits: semanticreview.ExposureResourceEstimate{RAMBytes: 1 << 30, ScratchBytes: 1 << 30, OutputBytes: 1 << 30, PixelCount: 200000000}, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha}}}}
	raw, _ := json.Marshal(config)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	path := filepath.Join(t.TempDir(), "exposure.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readPinnedExposureCatalog(path, digest)
	if err != nil || len(loaded.Tenants) != 1 || loaded.Tenants[0].TenantID != config.Tenants[0].TenantID {
		t.Fatal("catalog file load", err)
	}
	alias := path + "-symlink"
	if err = os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err = readPinnedExposureCatalog(alias, digest); err == nil {
		t.Fatal("symlinked catalog accepted")
	}
	if err = os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err = readPinnedExposureCatalog(path, digest); err == nil {
		t.Fatal("writable catalog accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readPinnedExposureCatalog(path, digest); err == nil {
		t.Fatal("changed raw bytes accepted")
	}
}

func TestSemanticExposureBootstrapRequiresCompleteSettings(t *testing.T) {
	for _, name := range []string{"PHOTO_SEMANTIC_EXPOSURE_CONFIG_FILE", "PHOTO_SEMANTIC_EXPOSURE_CONFIG_SHA256", "PHOTO_SEMANTIC_ADMISSION_DATABASE_URL"} {
		t.Setenv(name, "")
	}
	service, closeFn, err := buildSemanticExposureApply(context.Background(), nil, nil, "")
	if err != nil || service != nil || closeFn == nil {
		t.Fatal("disabled bootstrap performed installation", err)
	}
	closeFn()
	t.Setenv("PHOTO_SEMANTIC_ADMISSION_DATABASE_URL", "configured")
	if _, _, err = buildSemanticExposureApply(context.Background(), nil, nil, ""); err == nil {
		t.Fatal("unpaired admission settings accepted")
	}
}

func TestExposureStartupRejectsMultiplePreviewProviders(t *testing.T) {
	if _, _, err := buildSemanticExposureApply(context.Background(), nil, nil, "", nil, nil); err == nil {
		t.Fatal("ambiguous preview providers admitted")
	}
}
