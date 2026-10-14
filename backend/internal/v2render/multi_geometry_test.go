package v2render

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestMultiOutputGeometryPlanarAndFocus(t *testing.T) {
	for _, op := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, change := range []string{"valid", "color", "canvas", "matrix", "mask", "source_alias", "missing_registration", "unsafe_nonreference", "space_id"} {
			t.Run(op+"/"+change, func(t *testing.T) {
				v, _, _, _ := adoptedArrayFixture(t, op, adoptedArrayNPY("(100,100,3)", 3, 2), adoptedArrayNPY("(100,100)", 1, .5))
				proof := VerifiedMultiSourceClosure{originals: v, verified: true}
				sources, err := proof.Images()
				if err != nil {
					t.Fatal(err)
				}
				frames, reference, _, err := v.binding.Inputs()
				if err != nil {
					t.Fatal(err)
				}
				image := sources[reference]
				image.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("e", 64)}
				image.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}
				from := image.Canvas
				image.Canvas.SpaceID = "multi:test"
				matrix := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
				seed, err := canonical.Hash(map[string]any{"operation": op, "reference": reference, "sources": frames, "matrices": [][]float64{matrix, matrix}, "inverse_fields": []any{nil, nil}, "height": 100, "width": 100})
				if err != nil {
					t.Fatal(err)
				}
				image.Canvas.SpaceID = "multi:" + seed[:32]
				if change == "space_id" {
					image.Canvas.SpaceID = "multi:unbound"
				}
				var originalChain struct {
					Steps []json.RawMessage `json:"steps"`
					Hash  string            `json:"hash"`
				}
				if json.Unmarshal(image.TransformChain, &originalChain) != nil {
					t.Fatal("chain")
				}
				step, err := canonical.Marshal(map[string]any{"kind": "matrix", "from_canvas": from, "to_canvas": image.Canvas, "invertibility": "partial", "valid_region": map[string]any{"x": 0, "y": 0, "width": 100, "height": 100}, "forward_matrix": matrix})
				if err != nil {
					t.Fatal(err)
				}
				steps := append(originalChain.Steps, step)
				hash, err := canonical.Hash(map[string]any{"steps": steps})
				if err != nil {
					t.Fatal(err)
				}
				image.TransformChain, err = canonical.Marshal(map[string]any{"steps": steps, "hash": hash})
				if err != nil {
					t.Fatal(err)
				}
				mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "changed", Confidence: 1}
				target, err := canonical.Hash(image.Canvas)
				if err != nil {
					t.Fatal(err)
				}
				registrations := make([][]byte, len(frames))
				for i, frame := range frames {
					h := append([]float64(nil), matrix...)
					if change == "matrix" && i == reference {
						h[2] = 1
					}
					if change == "unsafe_nonreference" && i != reference {
						h = []float64{1e8, 0, 0, 0, 1e-8, 0, 0, 0, 1}
					}
					registrations[i], err = canonical.Marshal(MultiRegistration{SchemaVersion: 1, Source: frame, ToCanvasHash: target, Homography: h})
					if err != nil {
						t.Fatal(err)
					}
				}
				switch change {
				case "color":
					image.Color.Encoding = "gamma24"
				case "canvas":
					image.Canvas.SourceSHA256 = strings.Repeat("9", 64)
				case "mask":
					mask.Selector = "protected"
				case "source_alias":
					image.Pixels = sources[0].Pixels
				case "missing_registration":
					registrations = registrations[:1]
				}
				imageRaw, err := canonical.Marshal(image)
				if err != nil {
					t.Fatal(err)
				}
				maskRaw, err := canonical.Marshal(mask)
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, err = ValidateMultiOutputGeometry(proof, imageRaw, maskRaw, registrations)
				if change == "valid" {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, v2input.ErrInvalid) {
					t.Fatalf("%s: %v", change, err)
				}
			})
		}
	}
}
func TestMultiOutputGeometryRefusesUnverifiedSources(t *testing.T) {
	if _, _, _, err := ValidateMultiOutputGeometry(VerifiedMultiSourceClosure{}, nil, nil, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero proof accepted")
	}
}
