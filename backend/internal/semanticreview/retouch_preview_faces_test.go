package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchFaceDecoderBindsExactLegacyGeometryAndBytes(t *testing.T) {
	fixture, e := os.ReadFile("../review/testdata/working-images-python.json")
	if e != nil {
		t.Fatal(e)
	}
	var sources []json.RawMessage
	if json.Unmarshal(fixture, &sources) != nil {
		t.Fatal("fixture")
	}
	source := sources[0]
	image, e := review.ParseWorkingImage(source)
	if e != nil {
		t.Fatal(e)
	}
	hash := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	p := retouchParamsFixture()
	p.Source.Source = v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(source)}
	p.Source.CanvasHash, _ = canonical.Hash(image.Canvas)
	points := make([]review.Landmark, 68)
	for i := range points {
		points[i] = review.Landmark{X: .5, Y: .5, Confidence: 1}
	}
	face := review.Face68{ObjectID: uuid.New(), Points: points, Canvas: image.Canvas, TransformChain: image.TransformChain}
	raw, e := canonical.Marshal(face)
	if e != nil {
		t.Fatal(e)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(raw)}
	got, e := DecodeRetouchPreviewFaces(p, source, ref, raw)
	if e != nil || len(got) != 1 || got[0].ObjectID != nil || string(got[0].Skin) != "null" {
		t.Fatal("valid legacy face refused", e)
	}
	for _, failure := range []string{"bytes", "source", "geometry", "noncanonical_pinned", "pretty_pinned"} {
		body := append([]byte(nil), raw...)
		src := source
		pin := ref
		switch failure {
		case "bytes":
			body = append(body, ' ')
		case "noncanonical_pinned":
			body = append(body, ' ')
			pin.SHA256 = hash(body)
		case "pretty_pinned":
			var value any
			if err := json.Unmarshal(body, &value); err != nil { t.Fatal(err) }
			body, e = json.MarshalIndent(value, "", "  ")
			if e != nil { t.Fatal(e) }
			pin.SHA256 = hash(body)
		case "source":
			src = append(append([]byte(nil), source...), ' ')
		case "geometry":
			face.Points[0].X = 2
			body, _ = canonical.Marshal(face)
			pin.SHA256 = hash(body)
		}
		if faces, e := DecodeRetouchPreviewFaces(p, src, pin, body); e == nil || faces != nil {
			t.Fatal("invalid face escaped", failure)
		}
	}
}

func TestRetouchFaceDecoderCompleteCollection(t *testing.T) {
	for _, mode := range []string{"valid", "maximum", "too_many", "empty", "duplicate", "last_geometry", "source_pin", "mean", "noncanonical"} {
		t.Run(mode, func(t *testing.T) {
			fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
			if err != nil { t.Fatal(err) }
			var sources []json.RawMessage
			if err := json.Unmarshal(fixture, &sources); err != nil { t.Fatal(err) }
			source := sources[0]
			image, err := review.ParseWorkingImage(source)
			if err != nil { t.Fatal(err) }
			hash := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
			params := retouchParamsFixture()
			params.Source.Source = v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(source)}
			params.Source.CanvasHash, err = canonical.Hash(image.Canvas)
			if err != nil { t.Fatal(err) }
			count := 2
			if mode == "maximum" { count = 64 }
			if mode == "too_many" { count = 65 }
			if mode == "empty" { count = 0 }
			faces := make([]review.Face68, count)
			for i := range faces {
				points := make([]review.Landmark, 68)
				for j := range points { points[j] = review.Landmark{X: .5, Y: .5, Confidence: 1} }
				faces[i] = review.Face68{ObjectID: uuid.New(), Points: points, Canvas: image.Canvas, TransformChain: image.TransformChain}
			}
			if mode == "duplicate" { faces[1].ObjectID = faces[0].ObjectID }
			if mode == "last_geometry" { faces[len(faces)-1].Points[67].X = 2 }
			imageRef := review.ArtifactRef{ID: params.Source.Source.ArtifactID, SHA256: params.Source.Source.SHA256}
			if mode == "source_pin" { imageRef.ID = uuid.New() }
			mean := 1.0
			warnings := []string{}
			if mode == "empty" { mean = 0; warnings = []string{"no_faces"} }
			if mode == "mean" { mean = .9 }
			doc := map[string]any{"image": imageRef, "faces": review.ArtifactRef{ID: uuid.New(), SHA256: params.Source.Source.SHA256}, "landmarks": faces, "model_id": "fan2d", "model_lock_id": params.ModelLockSHA, "backend": "cpu", "preprocessing_id": "fan2d_crop256_v1", "mean_confidence": mean, "warnings": warnings}
			raw, err := canonical.Marshal(doc)
			if err != nil { t.Fatal(err) }
			if mode == "noncanonical" { raw = append(raw, ' ') }
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hash(raw)}
			got, err := DecodeRetouchPreviewFaces(params, source, ref, raw)
			if mode != "valid" && mode != "maximum" {
				if err == nil || got != nil { t.Fatal("invalid full catalog escaped", mode) }
				return
			}
			if err != nil || len(got) != count { t.Fatal("complete catalog refused", err) }
			for i := range got {
				if got[i].Landmarks != ref || got[i].ObjectID == nil || *got[i].ObjectID != faces[i].ObjectID || string(got[i].Skin) != "null" { t.Fatal("face identity lost", i) }
			}
			first := *got[0].ObjectID
			*got[len(got)-1].ObjectID = uuid.New()
			if *got[0].ObjectID != first { t.Fatal("collection object pointers alias") }
		})
	}
}
