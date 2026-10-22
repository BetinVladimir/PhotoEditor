package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestRetouchTenantInstallationValidatesOwnedCPUConfiguration(t *testing.T) {
	base := cleanupTenantCatalogFixture()
	cleanup := base.Tenants[0].BackgroundCleanupPreview
	pin := RetouchQualificationPin(cleanup.Qualification)
	pin.Report.ArtifactID = uuid.New()
	pin.Calibration.ArtifactID = uuid.New()
	limits := cleanup.Limits
	limits.RAMBytes = 4 << 30
	beauty := RetouchPreviewBeauty{Enabled: true, Algorithm: "beautyfilter_v1", Smoothing: .5, ToneEvenness: .2, TextureKeep: .8, SkinMask: "face68_conservative_v1", ProtectFeatures: true}
	config := RetouchPreviewDeployment{Qualification: pin, OwnerCosts: cleanup.OwnerCosts, Limits: limits, Beauty: beauty}
	for _, failure := range []string{"valid", "vram", "ram", "beauty", "backend"} {
		value := config
		switch failure {
		case "vram":
			value.Limits.VRAMBytes = 1
		case "ram":
			value.Limits.RAMBytes = 1
		case "beauty":
			value.Beauty.TextureKeep = 2
		case "backend":
			value.Qualification.Backend = "cuda"
		}
		base.Tenants[0].RetouchPreview = &value
		raw, e := json.Marshal(base)
		if e != nil {
			t.Fatal(e)
		}
		owned, e := DecodeExposurePreparationCatalog(raw)
		if (e == nil) != (failure == "valid") {
			t.Fatal(failure, e)
		}
		if failure == "valid" {
			value.Beauty.Smoothing = 0
			if owned.Tenants[0].RetouchPreview.Beauty.Smoothing != .5 {
				t.Fatal("borrowed config")
			}
		}
	}
}
