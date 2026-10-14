package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestSemanticMaskPinnedGeometryAndProtection(t *testing.T) {
	raw, _ := os.ReadFile("../review/testdata/working-images-python.json")
	var fixtures []json.RawMessage
	json.Unmarshal(raw, &fixtures)
	imageBody := fixtures[0]
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(imageBody)
	canvas, _ := canonical.Hash(image.Canvas)
	binding := SessionVersionBinding{uuid.New(), Ref{uuid.New(), hex.EncodeToString(digest[:])}, canvas}
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1, Protected: true}
	body, _ := canonical.Marshal(mask)
	sum := sha256.Sum256(body)
	maskRef := Ref{uuid.New(), hex.EncodeToString(sum[:])}
	pixels, err := ResolveSemanticMaskPixels(maskRef, body, binding, imageBody, true)
	if err != nil || pixels.ArtifactID != image.Pixels.ID {
		t.Fatal("mask pixels", err)
	}
	if _, err := ResolveSemanticMaskPixels(maskRef, body, binding, imageBody, false); err == nil {
		t.Fatal("protected mask selected")
	}
	if _, err := ResolveSemanticMaskPixels(maskRef, append(body, ' '), binding, imageBody, true); err == nil {
		t.Fatal("changed pin")
	}
	mask.Canvas.SpaceID = "wrong"
	body, _ = canonical.Marshal(mask)
	sum = sha256.Sum256(body)
	maskRef.SHA256 = hex.EncodeToString(sum[:])
	if _, err := ResolveSemanticMaskPixels(maskRef, body, binding, imageBody, true); err == nil {
		t.Fatal("changed canvas")
	}
}
