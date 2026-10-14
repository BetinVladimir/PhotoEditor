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

func TestImportPortraitDetailParamsRebindsResolvedAndInlineMask(t *testing.T) {
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
	resolved := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("resolved"))}
	pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("pixels"))}
	mask := map[string]any{"artifact": pixels, "canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain), "selector": "teeth", "confidence": 1, "protected": false}
	raw, err := canonical.Marshal(map[string]any{"image": old.Source, "resolved": resolved, "regions": []any{map[string]any{"region_id": "teeth-1", "kind": "teeth", "mask": mask, "confirmed": false, "strength": 0.4}}, "protected": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	operation := &preparedLayerOperationStub{}
	codec := ImportPortraitDetailParamsCodec{Qualifier: operation, Masks: ImportMaskCodec{Qualifier: &importMaskQualifierStub{}}}
	calls := []string{}
	remap := func(ref v2input.Ref, kind string) (v2input.Ref, error) {
		calls = append(calls, kind)
		if (kind == "ResolvedStyle" && ref != resolved) || (kind == "MaskPixels" && ref != pixels) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}, nil
	}
	prepared, err := codec.RemapImportDocument(context.Background(), "Params", raw, old, target, remap)
	if err != nil || len(calls) != 2 || calls[0] != "ResolvedStyle" || calls[1] != "MaskPixels" || operation.calls != 1 || operation.versions[0] != "portrait.detail:1" {
		t.Fatal("typed portrait remap", calls, err)
	}
	var result struct {
		Image, Resolved v2input.Ref
		Regions         []struct {
			Strength float64         `json:"strength"`
			Mask     json.RawMessage `json:"mask"`
		} `json:"regions"`
	}
	if err := json.Unmarshal(prepared, &result); err != nil {
		t.Fatal(err)
	}
	if result.Image != target.Source || result.Resolved.ArtifactID == resolved.ArtifactID || len(result.Regions) != 1 || result.Regions[0].Strength != 0.4 {
		t.Fatal("authored portrait controls changed")
	}
	changed := map[string]any{"image": old.Source, "resolved": resolved, "regions": []any{map[string]any{"region_id": "teeth-1", "kind": "teeth", "mask": mask, "confirmed": true, "strength": 0.4}}, "protected": []any{}}
	confirmedRaw, err := canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.RemapImportDocument(context.Background(), "Params", confirmedRaw, old, target, remap); err == nil {
		t.Fatal("archived confirmation inherited")
	}
}
