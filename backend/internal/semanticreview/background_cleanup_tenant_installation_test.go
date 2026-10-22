package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func cleanupTenantCatalogFixture() ExposurePreparationCatalog {
	sha := strings.Repeat("a", 64)
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Backend: "cpu", Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha}
	costs := ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}
	limits := ExposureResourceEstimate{RAMBytes: 1 << 30, ScratchBytes: 1 << 30, OutputBytes: 1 << 30, PixelCount: 200000000}
	execution := ExposureDeploymentBudget{Qualification: pin, OwnerCosts: costs, Limits: limits, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha}
	// Cleanup has its own report and calibration, even when model hashes match.
	cleanupPin := BackgroundCleanupQualificationPin(pin)
	cleanupPin.Report.ArtifactID = uuid.New()
	cleanupPin.Calibration.ArtifactID = uuid.New()
	return ExposurePreparationCatalog{SchemaVersion: 1, Tenants: []ExposureTenantPreparation{{TenantID: uuid.New(), Review: ReviewPreparationConfig{SchemaVersion: 1, ModelSHA: sha, DatasetSHA: sha, DocumentBudgetBytes: 64 << 20}, Execution: execution, BackgroundCleanupPreview: &BackgroundCleanupPreviewDeployment{Qualification: cleanupPin, OwnerCosts: costs, Limits: limits, ContextPixels: 128, FeatherPixels: 8}}}}
}

func TestBackgroundCleanupTenantCatalogOwnsInstallation(t *testing.T) {
	config := cleanupTenantCatalogFixture()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := DecodeExposurePreparationCatalog(raw)
	if err != nil || owned.Tenants[0].BackgroundCleanupPreview == nil {
		t.Fatal("valid cleanup installation rejected", err)
	}
	if owned.Tenants[0].RemovalPreview != nil {
		t.Fatal("cleanup installed removal")
	}
	config.Tenants[0].BackgroundCleanupPreview.ContextPixels = 32
	if owned.Tenants[0].BackgroundCleanupPreview.ContextPixels != 128 {
		t.Fatal("decoded installation aliases caller")
	}
	config = cleanupTenantCatalogFixture()
	config.Tenants[0].BackgroundCleanupPreview = nil
	raw, _ = json.Marshal(config)
	owned, err = DecodeExposurePreparationCatalog(raw)
	if err != nil || owned.Tenants[0].BackgroundCleanupPreview != nil {
		t.Fatal("optional cleanup enabled without configuration", err)
	}
}

func TestBackgroundCleanupTenantCatalogRejectsInvalidInstallation(t *testing.T) {
	for name, mutate := range map[string]func(*BackgroundCleanupPreviewDeployment){
		"context":        func(c *BackgroundCleanupPreviewDeployment) { c.ContextPixels = 31 },
		"feather":        func(c *BackgroundCleanupPreviewDeployment) { c.FeatherPixels = 65 },
		"backend":        func(c *BackgroundCleanupPreviewDeployment) { c.Qualification.Backend = "unknown" },
		"cpu_device":     func(c *BackgroundCleanupPreviewDeployment) { c.MeasuredDeviceBytes = 1 },
		"missing_report": func(c *BackgroundCleanupPreviewDeployment) { c.Qualification.Report.ArtifactID = uuid.Nil },
		"ram":            func(c *BackgroundCleanupPreviewDeployment) { c.Limits.RAMBytes = 1 },
		"pixel_limit":    func(c *BackgroundCleanupPreviewDeployment) { c.Limits.PixelCount = 200000001 },
		"cuda_cap": func(c *BackgroundCleanupPreviewDeployment) {
			c.Qualification.Backend = "cuda"
			c.MeasuredDeviceBytes = 10000000001
			c.Limits.VRAMBytes = c.MeasuredDeviceBytes
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := cleanupTenantCatalogFixture()
			mutate(config.Tenants[0].BackgroundCleanupPreview)
			raw, _ := json.Marshal(config)
			if _, err := DecodeExposurePreparationCatalog(raw); err == nil {
				t.Fatal("invalid cleanup installation accepted")
			}
		})
	}
}

func TestBackgroundCleanupTenantCatalogMetalUsesMeasuredBudget(t *testing.T) {
	config := cleanupTenantCatalogFixture()
	cleanup := config.Tenants[0].BackgroundCleanupPreview
	cleanup.Qualification.Backend = "mps"
	cleanup.MeasuredDeviceBytes = 12000000000
	cleanup.Limits.VRAMBytes = 12000000000
	raw, _ := json.Marshal(config)
	if _, err := DecodeExposurePreparationCatalog(raw); err != nil {
		t.Fatal("Metal incorrectly capped at CUDA limit", err)
	}
	cleanup.Limits.VRAMBytes = 11999999999
	raw, _ = json.Marshal(config)
	if _, err := DecodeExposurePreparationCatalog(raw); err == nil {
		t.Fatal("Metal exceeded measured admission budget")
	}
}

func TestBackgroundCleanupTenantCatalogRejectsForeignQualificationArtifacts(t *testing.T) {
	for _, role := range []string{"report", "calibration"} {
		t.Run(role, func(t *testing.T) {
			config := cleanupTenantCatalogFixture()
			other := cleanupTenantCatalogFixture().Tenants[0]
			if role == "report" {
				other.BackgroundCleanupPreview.Qualification.Report = config.Tenants[0].BackgroundCleanupPreview.Qualification.Report
			} else {
				other.BackgroundCleanupPreview.Qualification.Calibration = config.Tenants[0].BackgroundCleanupPreview.Qualification.Calibration
			}
			config.Tenants = append(config.Tenants, other)
			raw, _ := json.Marshal(config)
			if _, err := DecodeExposurePreparationCatalog(raw); err == nil {
				t.Fatal("cross-tenant cleanup qualification accepted")
			}
		})
	}
}

func TestShownBackgroundCleanupCatalogRequiresCPUCopyBudget(t *testing.T) {
	config := cleanupTenantCatalogFixture()
	copyBudget := config.Tenants[0].Execution
	copyBudget.Qualification.Report.ArtifactID = uuid.New()
	copyBudget.Qualification.Calibration.ArtifactID = uuid.New()
	config.Tenants[0].ShownBackgroundCleanupExecution = &copyBudget
	raw, _ := json.Marshal(config)
	owned, err := DecodeExposurePreparationCatalog(raw)
	if err != nil || owned.Tenants[0].ShownBackgroundCleanupExecution == nil {
		t.Fatal("cleanup copy budget rejected", err)
	}
	copyBudget.Qualification.Backend = "cuda"
	raw, _ = json.Marshal(config)
	if _, err = DecodeExposurePreparationCatalog(raw); err == nil {
		t.Fatal("GPU copy budget accepted")
	}
	if owned.Tenants[0].ShownBackgroundCleanupExecution.Qualification.Backend != "cpu" {
		t.Fatal("decoded copy budget aliases caller")
	}
}
