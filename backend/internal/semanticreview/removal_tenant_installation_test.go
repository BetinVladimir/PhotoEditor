package semanticreview

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRemovalTenantCatalogOwnsCopyBudgetAndRejectsCrossTenantRefs(t *testing.T) {
	sha := strings.Repeat("a", 64)
	config := ExposureDeploymentBudget{
		Qualification: ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Backend: "cpu", Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha},
		OwnerCosts:    ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20},
		Limits:        ExposureResourceEstimate{RAMBytes: 1 << 30, ScratchBytes: 1 << 30, OutputBytes: 1 << 30, PixelCount: 200000000},
		ModelLockSHA:  sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
	}

	copyBudget := config
	catalog := ExposurePreparationCatalog{SchemaVersion: 1, Tenants: []ExposureTenantPreparation{{TenantID: uuid.New(), Review: ReviewPreparationConfig{SchemaVersion: 1, ModelSHA: sha, DatasetSHA: sha, DocumentBudgetBytes: 64 << 20}, Execution: config, ShownRemovalExecution: &copyBudget}}}
	raw, _ := json.Marshal(catalog)
	owned, err := DecodeExposurePreparationCatalog(raw)
	if err != nil || owned.Tenants[0].ShownRemovalExecution == nil {
		t.Fatal("valid removal configuration rejected", err)
	}
	copyBudget.Qualification.Backend = "cuda"
	if owned.Tenants[0].ShownRemovalExecution.Qualification.Backend != "cpu" {
		t.Fatal("decoded catalog aliases caller")
	}
	raw, _ = json.Marshal(catalog)
	if _, err := DecodeExposurePreparationCatalog(raw); !errors.Is(err, ErrInvalid) {
		t.Fatal("CUDA copy budget accepted", err)
	}
	copyBudget = config
	other := catalog.Tenants[0]
	other.TenantID = uuid.New()
	// Distinct primary refs isolate the cross-tenant collision to removal copy.
	other.Execution.Qualification.Report.ArtifactID = uuid.New()
	other.Execution.Qualification.Calibration.ArtifactID = uuid.New()
	catalog.Tenants = append(catalog.Tenants, other)
	raw, _ = json.Marshal(catalog)
	if _, err := DecodeExposurePreparationCatalog(raw); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign removal qualification refs accepted", err)
	}
}
