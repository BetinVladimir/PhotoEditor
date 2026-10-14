package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestSemanticSourceMetadataPinsDocumentAndCanvas(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	json.Unmarshal(raw, &fixtures)
	body := fixtures[0]
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvas, _ := canonical.Hash(image.Canvas)
	sum := sha256.Sum256(body)
	binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), hex.EncodeToString(sum[:])}, canvas}
	metadata, err := ResolveSemanticSourceMetadata(binding, body)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Width != int64(image.Canvas.Width) || metadata.Height != int64(image.Canvas.Height) || !metadata.Gamma || metadata.Inputs.Pixels.ArtifactID != image.Pixels.ID {
		t.Fatal("source metadata")
	}
	changed := append([]byte(nil), body...)
	changed = append(changed, ' ')
	if _, err := ResolveSemanticSourceMetadata(binding, changed); err == nil {
		t.Fatal("changed bytes")
	}
	binding.CanvasHash = strings.Repeat("f", 64)
	if _, err := ResolveSemanticSourceMetadata(binding, body); err == nil {
		t.Fatal("changed canvas")
	}
}

func TestSemanticSourceRejectsIncompatibleProcessingInputs(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	baseline, err := review.ParseWorkingImage(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"linear_srgb", "linear_rec2020", "uint16", "short_canvas", "invalid_icc"} {
		t.Run(name, func(t *testing.T) {
			image := baseline
			switch name {
			case "linear_srgb":
				image.Color.ProfileID, image.Color.Encoding = "linear_srgb_v1", "linear"
			case "linear_rec2020":
				image.Color.Encoding = "linear"
			case "uint16":
				image.DType = "uint16"
			case "short_canvas":
				image.Canvas.Width = 7
			case "invalid_icc":
				image.Color.ICCSHA256 = "invalid"
			}
			body, err := canonical.Marshal(image)
			if err != nil {
				t.Fatal(err)
			}
			canvas, err := canonical.Hash(image.Canvas)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(body)
			binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), hex.EncodeToString(sum[:])}, canvas}
			if _, err := ResolveSemanticSourceMetadata(binding, body); err == nil {
				t.Fatal("incompatible image admitted with valid content and canvas pins")
			}
		})
	}
}

func TestSemanticGeometryArraysUsePinnedStepCanvas(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	for _, body := range images {
		image, err := review.ParseWorkingImage(body)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		canvas, _ := canonical.Hash(image.Canvas)
		binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), hex.EncodeToString(sum[:])}, canvas}
		fields, err := ResolveSemanticGeometryArrays(binding, body)
		if err != nil {
			t.Fatal(err)
		}
		var chain struct {
			Steps []struct {
				Kind    string        `json:"kind"`
				To      review.Canvas `json:"to_canvas"`
				Inverse *Ref          `json:"inverse_field"`
			}
		}
		if err := json.Unmarshal(image.TransformChain, &chain); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, step := range chain.Steps {
			if step.Kind == "dense_warp" {
				if len(fields) <= count || fields[count].Ref != *step.Inverse || fields[count].Width != int64(step.To.Width) || fields[count].Height != int64(step.To.Height) {
					t.Fatal("inverse grid binding")
				}
				count++
			}
		}
		if len(fields) != count {
			t.Fatal("extra inverse fields")
		}
		if _, err := ResolveSemanticGeometryArrays(binding, append(append(json.RawMessage(nil), body...), ' ')); err == nil {
			t.Fatal("changed geometry source pin")
		}
	}
}
