package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestRemovalPreviewDeploymentUsesBackendSpecificMeasuredLimits(t *testing.T) {
	sha := strings.Repeat("a", 64)
	config := RemovalPreviewDeployment{
		Qualification:       RemovalQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Backend: "mps", Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha},
		OwnerCosts:          ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20},
		Limits:              ExposureResourceEstimate{RAMBytes: 2 << 30, VRAMBytes: 12000000000, ScratchBytes: 1 << 30, OutputBytes: 1 << 30, PixelCount: 200000000},
		MeasuredDeviceBytes: 11000000000, ContextPixels: 64, FeatherPixels: 8,
	}
	if err := validateRemovalPreviewDeployment(config); err != nil {
		t.Fatal("Metal incorrectly capped at 10 GB", err)
	}
	config.Qualification.Backend = "cuda"
	if err := validateRemovalPreviewDeployment(config); err == nil {
		t.Fatal("CUDA over 10 GB accepted")
	}
	config.MeasuredDeviceBytes = 10000000000
	if err := validateRemovalPreviewDeployment(config); err != nil {
		t.Fatal("CUDA 10 GB rejected", err)
	}
	config.Qualification.Backend = "cpu"
	if err := validateRemovalPreviewDeployment(config); err == nil {
		t.Fatal("CPU device allocation accepted")
	}
	config.MeasuredDeviceBytes = 0
	if err := validateRemovalPreviewDeployment(config); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"context", "feather", "pixels", "ram"} {
		changed := config
		switch field {
		case "context":
			changed.ContextPixels = 513
		case "feather":
			changed.FeatherPixels = 0
		case "pixels":
			changed.Limits.PixelCount = 200000001
		case "ram":
			changed.Limits.RAMBytes = 1
		}
		if err := validateRemovalPreviewDeployment(changed); err == nil {
			t.Fatal("invalid deployment accepted", field)
		}
	}
}
