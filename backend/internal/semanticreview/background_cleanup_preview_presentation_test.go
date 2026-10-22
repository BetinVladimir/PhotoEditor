package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestBackgroundCleanupPresentationBindsNumericalEvidenceAndDistinctFourMedia(t *testing.T) {
	params := BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}
	newRef := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: params.Plan.SHA256} }
	expected := VerifiedBackgroundCleanupPreviewEvidence{Evidence: BackgroundCleanupPreviewEvidence{Params: params, WorkingImage: newRef(), Pixels: newRef()}, Source: PlanSource{Ref: newRef()}}
	for _, mutation := range []string{"valid", "evidence", "alias", "icc", "width", "overlay", "params"} {
		p := BackgroundCleanupPreviewPresentation{SchemaVersion: 1, Params: params, NumericalEvidence: expected.Source.Ref, WorkingImage: expected.Evidence.WorkingImage, Pixels: expected.Evidence.Pixels,
			Before: newRef(), After: newRef(), Affected: newRef(), Protected: newRef(), Width: 2, Height: 1, ICCLockSHA: params.Plan.SHA256, SRGBSHA: params.Plan.SHA256, Resize: "area_linear_premult_v1", Overlay: "max_coverage_ceil_alpha_v1"}
		switch mutation {
		case "evidence":
			p.NumericalEvidence = newRef()
		case "alias":
			p.Protected = p.After
		case "icc":
			p.SRGBSHA = "wrong"
		case "width":
			p.Width = 2561
		case "overlay":
			p.Overlay = "average"
		case "params":
			p.Params.TargetRevision = "2"
		}
		raw, err := canonical.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		ref := newRef()
		ref.SHA256 = hex.EncodeToString(sum[:])
		_, err = DecodeBackgroundCleanupPreviewPresentation(ref, raw, expected, params.Plan.SHA256, params.Plan.SHA256)
		if (err == nil) != (mutation == "valid") {
			t.Fatal(mutation, err)
		}
	}
}
