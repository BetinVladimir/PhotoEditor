package interchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestImportDefectRepairParamsRebindsTypedDependencies(t *testing.T) {
	_, _, imageRaw := importWorkingValidatorFixture(t)
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(imageRaw)}, CanvasHash: canvas}
	target := old
	target.AssetVersionID = uuid.New()
	target.Source.ArtifactID = uuid.New()
	ref := func(label string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(label))}
	}
	proposal, decision, resolved, pixels := ref("proposal"), ref("decision"), ref("resolved"), ref("pixels")
	mask := map[string]any{"artifact": pixels, "canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain), "object_id":nil,"model_lock_id":nil,"selector": "face", "confidence": 1, "protected": true}
	raw, err := canonical.Marshal(map[string]any{"image": old.Source, "proposal": proposal, "review": decision, "resolved": resolved, "protected": mask})
	if err != nil {
		t.Fatal(err)
	}
	operation := &preparedLayerOperationStub{}
	codec := ImportDefectRepairParamsCodec{Qualifier: operation, Masks: ImportMaskCodec{Qualifier: &importMaskQualifierStub{}}}
	want := []string{"DefectCandidates", "DefectReview", "ResolvedStyle", "MaskPixels"}
	var calls []string
	resolve := func(archived v2input.Ref, kind string) (v2input.Ref, error) {
		calls = append(calls, kind)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: archived.SHA256}, nil
	}
	prepared, err := codec.RemapImportDocument(context.Background(), "Params", raw, old, target, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != len(want) || operation.calls != 1 || operation.versions[0] != "defects.repair:1" {
		t.Fatalf("typed dependencies or qualification missing: %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("dependency %d: got %q, want %q", i, calls[i], want[i])
		}
	}
	var result struct {
		Image    v2input.Ref `json:"image"`
		Proposal v2input.Ref `json:"proposal"`
		Review   v2input.Ref `json:"review"`
		Resolved v2input.Ref `json:"resolved"`
	}
	if err := json.Unmarshal(prepared, &result); err != nil || result.Image != target.Source || result.Proposal.ArtifactID == proposal.ArtifactID || result.Review.ArtifactID == decision.ArtifactID || result.Resolved.ArtifactID == resolved.ArtifactID {
		t.Fatal("archived dependency retained", err)
	}
	wrong := target
	wrong.Source = old.Source
	if err := codec.QualifyRemappedImportDocument(context.Background(), "Params", prepared, wrong); err == nil {
		t.Fatal("accepted stale target image")
	}
}
