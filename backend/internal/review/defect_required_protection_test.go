package review

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestNativeDefectParamsCannotDropRequiredProtection(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if err := decodeExact(raw, &report); err != nil {
		t.Fatal(err)
	}
	report.Params.Permanent = &ProtectionMask{Artifact: ArtifactRef{ID: uuid.New(), SHA256: report.Params.Image.SHA256}, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain, Selector: "protected", Confidence: 1, Protected: true}
	source := DefectSource{Image: report.Params.Image, Pixels: ArtifactRef{ID: uuid.New(), SHA256: report.Params.Image.SHA256}, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	required := []ProtectionMask{*report.Params.Permanent}
	verify := func(ctx context.Context, params proposalReportParams) error {
		body, err := canonical.Marshal(params)
		if err != nil {
			return err
		}
		digest, err := canonical.Hash(params)
		if err != nil {
			return err
		}
		return VerifyDefectRequiredProtections(ctx, ArtifactRef{ID: uuid.New(), SHA256: digest}, body, source, required)
	}
	if err := verify(context.Background(), report.Params); err != nil {
		t.Fatal(err)
	}
	dropped := report.Params
	dropped.Permanent = nil
	if err := verify(context.Background(), dropped); err == nil {
		t.Fatal("dropped required protection accepted")
	}
	substituted := report.Params
	mask := *report.Params.Permanent
	mask.Artifact.ID = uuid.New()
	substituted.Permanent = &mask
	if err := verify(context.Background(), substituted); err == nil {
		t.Fatal("substituted protection accepted")
	}
	changedBinding := report.Params
	changedMask := *report.Params.Permanent
	changedMask.Confidence = .5
	changedBinding.Permanent = &changedMask
	if err := verify(context.Background(), changedBinding); err == nil {
		t.Fatal("changed required protection binding accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verify(ctx, report.Params); err != context.Canceled {
		t.Fatal(err)
	}
}
