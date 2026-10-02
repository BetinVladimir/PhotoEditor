package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type maskProvenanceFixture struct {
	reject bool
	calls  int
}

func (p *maskProvenanceFixture) Verify(_ context.Context, _ TrainingStart,
	_ TrainingCandidate, _, _ v2input.Ref, _ bool) error {
	p.calls++
	if p.reject {
		return ErrTrainingDraftConflict
	}
	return nil
}

func TestTrainingMaskSourcePinsProtectedMaskAndPixelChild(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(fixture, &images); err != nil || len(images) == 0 {
		t.Fatal("missing working image fixture", err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	pixel := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("1", 64)}
	mask := map[string]any{
		"artifact": pixel, "canvas": image.Canvas,
		"transform_chain": json.RawMessage(image.TransformChain),
		"object_id":       nil, "selector": "manual", "confidence": 1.0,
		"model_lock_id": nil, "protected": true,
	}
	body, err := canonical.Marshal(mask)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	store := trainingRecipeStore{objects: map[uuid.UUID]objectstore.Object{},
		bodies: map[string][]byte{"mask": body}}
	store.objects[ref.ArtifactID] = objectstore.Object{Key: "mask",
		SHA256: ref.SHA256, Bytes: int64(len(body)),
		VersionID: "v1", ContentType: "application/json"}
	store.objects[pixel.ArtifactID] = objectstore.Object{Key: "pixels",
		SHA256: pixel.SHA256, Bytes: 128,
		VersionID: "v1", ContentType: "application/octet-stream"}
	provenance := &maskProvenanceFixture{}
	source := TrainingMaskSource{Catalog: store, Objects: store,
		Provenance: provenance}
	start := TrainingStart{TenantID: uuid.New(), ProjectID: uuid.New()}
	candidate := TrainingCandidate{EventID: uuid.New(), AssetVersionID: uuid.New()}
	bound := TrainingImage{Document: image,
		PixelsCount: image.Canvas.Width * image.Canvas.Height}
	got, err := source.Load(context.Background(), auth.Identity{},
		start, candidate, ref, bound, true)
	if err != nil || got.Pixels != pixel || provenance.calls != 1 {
		t.Fatalf("protected mask not resolved: %v", err)
	}
	if _, err := source.Load(context.Background(), auth.Identity{},
		start, candidate, ref, bound, false); err == nil {
		t.Fatal("protected mask accepted as editable mask")
	}
	provenance.reject = true
	if _, err := source.Load(context.Background(), auth.Identity{},
		start, candidate, ref, bound, true); err == nil {
		t.Fatal("mask without producer provenance accepted")
	}
	provenance.reject = false
	delete(store.objects, pixel.ArtifactID)
	if _, err := source.Load(context.Background(), auth.Identity{},
		start, candidate, ref, bound, true); err == nil {
		t.Fatal("missing mask pixel child accepted")
	}
}
