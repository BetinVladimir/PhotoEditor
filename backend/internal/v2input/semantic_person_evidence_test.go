package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestPersonEvidenceCrossDocumentBinding(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if json.Unmarshal(raw, &fixtures) != nil {
		t.Fatal("fixture")
	}
	imageBody := fixtures[0]
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(imageBody)
	canvas, _ := canonical.Hash(image.Canvas)
	binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), hex.EncodeToString(digest[:])}, canvas}
	model := strings.Repeat("a", 64)
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: .9, ModelLockID: &model, Protected: false}

	maskBody, _ := canonical.Marshal(mask)
	sum := sha256.Sum256(maskBody)
	root := Ref{uuid.New(), hex.EncodeToString(sum[:])}
	metadata, _ := ResolveSemanticSourceMetadata(binding, imageBody)
	scale := 768 / math.Max(float64(metadata.Width), float64(metadata.Height))
	p := semanticPersonParams{binding.Source, "lraspp_mobilenet_v3_large", model, "person_guided_v1", 768, 512}
	r := semanticPersonReport{Image: binding.Source, Mask: Ref{image.Pixels.ID, image.Pixels.SHA256}, Width: int(math.Max(1, math.RoundToEven(float64(metadata.Width)*scale))), Height: int(math.Max(1, math.RoundToEven(float64(metadata.Height)*scale))), Model: p.Model, Lock: model, Backend: "cuda", Preprocessing: p.Preprocessing, Class: 15, Confidence: .9, Fraction: .2, Warnings: []string{}}
	verify := func(params semanticPersonParams, report semanticPersonReport) error {
		pb, _ := canonical.Marshal(params)
		rb, _ := canonical.Marshal(report)
		return VerifySemanticPersonEvidence(binding, imageBody, root, maskBody, pb, rb, model, "cuda")
	}
	if err := verify(p, r); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*semanticPersonReport){
		func(x *semanticPersonReport) { x.Image.ArtifactID = uuid.New() },
		func(x *semanticPersonReport) { x.Mask.ArtifactID = uuid.New() },
		func(x *semanticPersonReport) { x.Lock = strings.Repeat("b", 64) },
		func(x *semanticPersonReport) { x.Backend = "cpu" },
		func(x *semanticPersonReport) { x.Width-- },
		func(x *semanticPersonReport) { x.Class = 14 },
		func(x *semanticPersonReport) { x.Confidence = .8 },
		func(x *semanticPersonReport) { x.Fraction = -1 },
		func(x *semanticPersonReport) { x.Warnings = nil },
		func(x *semanticPersonReport) { x.Clip = .1 },
	} {
		x := r
		change(&x)
		if verify(p, x) == nil {
			t.Fatal("report substitution")
		}
	}
	for _, change := range []func(*semanticPersonParams){func(x *semanticPersonParams) { x.Image.ArtifactID = uuid.New() }, func(x *semanticPersonParams) { x.LongSide = 1024 }, func(x *semanticPersonParams) { x.GuidedTile = 128 }} {
		x := p
		change(&x)
		if verify(x, r) == nil {
			t.Fatal("params substitution")
		}
	}
	pb, _ := canonical.Marshal(p)
	rb, _ := canonical.Marshal(r)
	for _, bad := range [][]byte{append(append([]byte(nil), rb...), ' '), []byte(`{}`), []byte(`{"image":null}`)} {
		if VerifySemanticPersonEvidence(binding, imageBody, root, maskBody, pb, bad, model, "cuda") == nil {
			t.Fatal("invalid report accepted")
		}
	}
}
