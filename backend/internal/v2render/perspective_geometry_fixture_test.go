package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

// Fixture deliberately retains a dense original transform before the final
// perspective matrix. No pixels are synthesized or quality claims made.
func perspectiveGeometryFixture(t *testing.T) (AdoptedPerspectiveBinding, v2draft.ImageResultDocument, []byte, map[string][]byte) {
	t.Helper()
	raw, sourceRef, _ := originalDenseFixture(t, false)
	source, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	sourceRef.SHA256, err = canonical.Hash(source)
	if err != nil {
		t.Fatal(err)
	}
	from, err := canonical.Hash(source.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := AdoptedPerspectiveBinding{Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceRef, CanvasHash: from}}
	newRef := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)} }
	result := v2draft.ImageResultDocument{WorkingImage: newRef(), ChangedMask: newRef(), Report: newRef()}
	target := source
	target.Canvas.Width = 6
	target.Canvas.Height = 5
	target.Canvas.SpaceID = "perspective:fixture"
	target.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("1", 64)}
	alpha := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("2", 64)}
	target.Alpha = &alpha
	matrix := []float64{1, 0, 1, 0, 1, 1, 0, 0, 1}
	var chain struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if err := json.Unmarshal(source.TransformChain, &chain); err != nil {
		t.Fatal(err)
	}
	step, err := canonical.Marshal(map[string]any{"kind": "matrix", "from_canvas": source.Canvas, "to_canvas": target.Canvas, "invertibility": "partial", "valid_region": map[string]any{"x": 0, "y": 0, "width": 6, "height": 5}, "forward_matrix": matrix})
	if err != nil {
		t.Fatal(err)
	}
	chain.Steps = append(chain.Steps, step)
	chain.Hash, err = canonical.Hash(map[string]any{"steps": chain.Steps})
	if err != nil {
		t.Fatal(err)
	}
	target.TransformChain, err = canonical.Marshal(chain)
	if err != nil {
		t.Fatal(err)
	}
	to, err := canonical.Hash(target.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	transform := newRef()
	docs := map[string][]byte{}
	put := func(ref v2input.Ref, value any) {
		body, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		docs[ref.ArtifactID.String()] = body
	}
	put(result.WorkingImage, target)
	put(result.ChangedMask, review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("3", 64)}, Canvas: target.Canvas, TransformChain: target.TransformChain, Selector: "changed", Confidence: 1})
	put(transform, map[string]any{"schema_version": 1, "from_canvas_hash": from, "to_canvas_hash": to, "matrix": matrix, "kind": "homography"})
	put(result.Report, map[string]any{"schema_version": 1, "skipped_count": 0, "outside_mask_max_error": 0, "clipping_fraction": 0, "operation": "optical.perspective", "status": "review", "reason": "perspective_manual", "sources": []v2input.SessionVersionBinding{binding.Base}, "transform": transform, "identity": nil, "registrations": []v2input.Ref{}})
	return binding, result, raw, docs
}

func TestPerspectiveResultGeometryRetainsOriginalChain(t *testing.T) {
	binding, result, raw, docs := perspectiveGeometryFixture(t)
	if err := verifyPerspectiveResultGeometry(binding, result, raw, docs); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"source", "color", "alpha", "chain", "report", "transform"} {
		t.Run(mutation, func(t *testing.T) {
			b, r, s, d := perspectiveGeometryFixture(t)
			var image review.WorkingImage
			if err := json.Unmarshal(d[r.WorkingImage.ArtifactID.String()], &image); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "source":
				image.Canvas.SourceSHA256 = strings.Repeat("9", 64)
			case "color":
				image.Color.ICCSHA256 = strings.Repeat("9", 64)
			case "alpha":
				image.Alpha = nil
			case "chain":
				image.TransformChain = json.RawMessage(`{"steps":[],"hash":"` + strings.Repeat("9", 64) + `"}`)
			case "report":
				d[r.Report.ArtifactID.String()] = []byte(`{"operation":"optical.perspective","status":"pass"}`)
			case "transform":
				var report struct {
					Transform v2input.Ref `json:"transform"`
				}
				if err := json.Unmarshal(d[r.Report.ArtifactID.String()], &report); err != nil {
					t.Fatal(err)
				}
				delete(d, report.Transform.ArtifactID.String())
			}
			if mutation != "report" && mutation != "transform" {
				var err error
				d[r.WorkingImage.ArtifactID.String()], err = canonical.Marshal(image)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := verifyPerspectiveResultGeometry(b, r, s, d); err == nil {
				t.Fatal("geometry substitution accepted")
			}
		})
	}
}
