package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestNativePersonProtectionProjectionPreservesOriginalInputs(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []json.RawMessage
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(fixtures[0])
	if err != nil {
		t.Fatal(err)
	}
	canvas, _ := canonical.Hash(image.Canvas)
	model := strings.Repeat("a", 64)
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: .83, ModelLockID: &model}
	body, _ := canonical.Marshal(mask)
	rootSHA, _ := canonical.Hash(mask)
	imageSHA, _ := canonical.Hash(image)
	p := nativePersonInputAuthority{body: string(body), root: v2input.Ref{ArtifactID: uuid.New(), SHA256: rootSHA}, pixels: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: imageSHA}, CanvasHash: canvas}}
	// Use exact canonical bytes matching the pinned source SHA.
	imageBody, _ := canonical.Marshal(image)
	got, err := projectNativePersonProtectionInput(context.Background(), p, imageBody)
	if err != nil {
		t.Fatal(err)
	}
	want := review.ProtectionMask{Artifact: mask.Artifact, Canvas: mask.Canvas, TransformChain: mask.TransformChain, Selector: mask.Selector, Confidence: mask.Confidence, ModelLockID: mask.ModelLockID, Protected: true}
	if !reflect.DeepEqual(got, want) || p.body != string(body) {
		t.Fatal("projection changed original input")
	}
	got.TransformChain[0] = '!'
	*got.ModelLockID = "changed"
	if p.body != string(body) || model != strings.Repeat("a", 64) {
		t.Fatal("projection aliases original")
	}
	for _, kind := range []string{"root", "pixels", "source", "canvas", "protected", "selector", "object", "model"} {
		changed := p
		m := mask
		switch kind {
		case "root":
			changed.root.SHA256 = strings.Repeat("b", 64)
		case "pixels":
			changed.pixels.ArtifactID = uuid.New()
		case "source":
			changed.binding.Source.SHA256 = strings.Repeat("b", 64)
		case "canvas":
			changed.binding.CanvasHash = strings.Repeat("b", 64)
		case "protected":
			m.Protected = true
		case "selector":
			m.Selector = "skin"
		case "object":
			id := uuid.New()
			m.ObjectID = &id
		case "model":
			m.ModelLockID = nil
		}
		if kind == "protected" || kind == "selector" || kind == "object" || kind == "model" {
			b, _ := canonical.Marshal(m)
			changed.body = string(b)
			changed.root.SHA256, _ = canonical.Hash(m)
		}
		if result, err := projectNativePersonProtectionInput(context.Background(), changed, imageBody); err == nil || result.Artifact.ID != uuid.Nil {
			t.Fatalf("%s accepted: %v", kind, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := projectNativePersonProtectionInput(ctx, p, imageBody); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
