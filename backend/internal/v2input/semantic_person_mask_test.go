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

func TestPersonMaskSourceModelAndSelectorBinding(t *testing.T) {
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
	verify := func(m review.MaskRef, expected string) (Ref, error) {
		body, _ := canonical.Marshal(m)
		sum := sha256.Sum256(body)
		return ResolveSemanticPersonMaskPixels(Ref{uuid.New(), hex.EncodeToString(sum[:])}, body, binding, imageBody, expected)
	}
	if pixels, err := verify(mask, model); err != nil || pixels.ArtifactID != image.Pixels.ID {
		t.Fatal(pixels, err)
	}
	for _, change := range []func(*review.MaskRef){
		func(m *review.MaskRef) { m.Selector = "manual" },
		func(m *review.MaskRef) { m.Protected = true },
		func(m *review.MaskRef) { m.ModelLockID = nil },
		func(m *review.MaskRef) { id := uuid.New(); m.ObjectID = &id },
		func(m *review.MaskRef) { m.Canvas.SpaceID = "foreign" },
	} {
		changed := mask
		change(&changed)
		if p, e := verify(changed, model); e == nil || p.ArtifactID != uuid.Nil {
			t.Fatal("invalid person evidence accepted", e)
		}
	}
	for _, lock := range []string{strings.Repeat("b", 64), "", strings.Repeat("A", 64)} {
		if p, e := verify(mask, lock); e == nil || p.ArtifactID != uuid.Nil {
			t.Fatal("model substitution accepted", e)
		}
	}
}
