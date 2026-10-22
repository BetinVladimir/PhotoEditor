package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func upscaleReportFixture() (map[string]any, v2input.Ref) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	return map[string]any{"image": ref, "model_id": "realesrgan_x2", "model_lock_id": strings.Repeat("b", 64), "backend": "mps", "scale": 2, "color_adapter_id": "work_srgb_delta_v1", "tile": 256, "overlap": 32, "input_pixel_count": 4, "output_pixel_count": 16, "disabled_delta_fraction": 0, "source_clip_fraction": 0, "gamut_excess_fraction": 0, "alpha_resampled": false, "warnings": []string{}}, ref
}

func TestUpscaleReportTraversalBindsSource(t *testing.T) {
	doc, source := upscaleReportFixture()
	raw, err := canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	children, err := DocumentChildren(raw, "UpscaleReport", parent)
	if err != nil || len(children) != 1 {
		t.Fatal(children, err)
	}
	child := children[0]
	if child.Ref != source || child.ParentRef != parent || child.Contract != "WorkingImage" || child.Path != "Document.image" {
		t.Fatal(child)
	}
}

func TestUpscaleReportTraversalRefusesMalformedContract(t *testing.T) {
	for _, change := range []string{"extra", "missing", "null", "bad-source", "bad-lock", "backend", "fraction", "pixels", "tile", "alpha", "warnings", "duplicate"} {
		t.Run(change, func(t *testing.T) {
			doc, _ := upscaleReportFixture()
			switch change {
			case "extra":
				doc["trusted"] = true
			case "missing":
				delete(doc, "scale")
			case "null":
				doc["source_clip_fraction"] = nil
			case "bad-source":
				doc["image"] = map[string]any{"artifact_id": uuid.Nil, "sha256": strings.Repeat("a", 64)}
			case "bad-lock":
				doc["model_lock_id"] = "bad"
			case "backend":
				doc["backend"] = "implicit"
			case "fraction":
				doc["gamut_excess_fraction"] = 1.01
			case "pixels":
				doc["output_pixel_count"] = 200000001
			case "tile":
				doc["tile"] = 255
			case "alpha":
				doc["alpha_resampled"] = "false"
			case "warnings":
				doc["warnings"] = []string{"unknown"}
			}
			raw, err := canonical.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if change == "duplicate" {
				var fields map[string]json.RawMessage
				json.Unmarshal(raw, &fields)
				raw = append([]byte(`{"scale":2,`), raw[1:]...)
			}
			parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
			if _, err := DocumentChildren(raw, "UpscaleReport", parent); err == nil {
				t.Fatal("accepted malformed report")
			}
		})
	}
}
