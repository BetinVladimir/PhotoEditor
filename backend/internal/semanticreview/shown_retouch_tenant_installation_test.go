package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestShownRetouchCatalogRequiresCPUCopyBudget(t *testing.T) {
	config := cleanupTenantCatalogFixture()
	copyBudget := config.Tenants[0].Execution
	copyBudget.Qualification.Report.ArtifactID = uuid.New()
	copyBudget.Qualification.Calibration.ArtifactID = uuid.New()
	config.Tenants[0].ShownRetouchExecution = &copyBudget
	raw, _ := json.Marshal(config)
	owned, err := DecodeExposurePreparationCatalog(raw)
	if err != nil || owned.Tenants[0].ShownRetouchExecution == nil {
		t.Fatal("retouch copy budget rejected", err)
	}
	copyBudget.Qualification.Backend = "cuda"
	raw, _ = json.Marshal(config)
	if _, err = DecodeExposurePreparationCatalog(raw); err == nil {
		t.Fatal("GPU copy budget accepted")
	}
	if owned.Tenants[0].ShownRetouchExecution.Qualification.Backend != "cpu" {
		t.Fatal("decoded copy budget aliases caller")
	}
}

func TestShownRetouchCatalogRejectsIncompleteOrMismatchedCopyDeployment(t *testing.T) {
	for _, mode := range []string{"model", "runtime", "device", "dataset", "report", "calibration", "policy", "metal", "vram", "ram", "scratch", "output"} {
		t.Run(mode, func(t *testing.T) {
			config := cleanupTenantCatalogFixture()
			budget := config.Tenants[0].Execution
			budget.Qualification.Report.ArtifactID = uuid.New()
			budget.Qualification.Calibration.ArtifactID = uuid.New()
			switch mode {
			case "model":
				budget.ModelLockSHA = strings.Repeat("b", 64)
			case "runtime":
				budget.RuntimeLockSHA = strings.Repeat("b", 64)
			case "device":
				budget.DeviceProfileSHA = strings.Repeat("b", 64)
			case "dataset":
				budget.DatasetSHA = strings.Repeat("b", 64)
			case "report":
				budget.Qualification.Report.ArtifactID = uuid.Nil
			case "calibration":
				budget.Qualification.Calibration.ArtifactID = uuid.Nil
			case "policy":
				budget.Qualification.PolicyID = uuid.Nil
			case "metal":
				budget.Qualification.Backend = "mps"
			case "vram":
				budget.Limits.VRAMBytes = 1
			case "ram":
				budget.Limits.RAMBytes = 1
			case "scratch":
				budget.Limits.ScratchBytes = 1
			case "output":
				budget.Limits.OutputBytes = 1
			}
			config.Tenants[0].ShownRetouchExecution = &budget
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = DecodeExposurePreparationCatalog(raw); err == nil {
				t.Fatal("invalid retouch copy deployment admitted")
			}
		})
	}
}

func TestShownRetouchCatalogSeparatesTenantQualificationArtifacts(t *testing.T) {
	first, second := cleanupTenantCatalogFixture(), cleanupTenantCatalogFixture()
	for _, config := range []*ExposurePreparationCatalog{&first, &second} {
		budget := config.Tenants[0].Execution
		budget.Qualification.Report.ArtifactID = uuid.New()
		budget.Qualification.Calibration.ArtifactID = uuid.New()
		config.Tenants[0].ShownRetouchExecution = &budget
	}
	first.Tenants = append(first.Tenants, second.Tenants[0])
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeExposurePreparationCatalog(raw); err != nil {
		t.Fatal("independent tenant installations rejected", err)
	}
	for _, field := range []string{"report", "calibration"} {
		changed := *first.Tenants[1].ShownRetouchExecution
		if field == "report" {
			changed.Qualification.Report = first.Tenants[0].ShownRetouchExecution.Qualification.Report
		} else {
			changed.Qualification.Calibration = first.Tenants[0].ShownRetouchExecution.Qualification.Calibration
		}
		first.Tenants[1].ShownRetouchExecution = &changed
		raw, err = json.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeExposurePreparationCatalog(raw); err == nil {
			t.Fatal("foreign tenant qualification artifact admitted", field)
		}
		first.Tenants[1].ShownRetouchExecution = second.Tenants[0].ShownRetouchExecution
	}
}
