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

func TestTrainingImageSourcePinsRootAndPixels(t *testing.T) {
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
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(images[0])
	rootRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	pixelRef := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: rootRef, CanvasHash: canvasSHA}
	store := trainingRecipeStore{objects: map[uuid.UUID]objectstore.Object{},
		bodies: map[string][]byte{"root": images[0]}}
	store.objects[rootRef.ArtifactID] = objectstore.Object{Key: "root",
		SHA256: rootRef.SHA256, Bytes: int64(len(images[0])),
		VersionID: "v1", ContentType: "application/json"}
	store.objects[pixelRef.ArtifactID] = objectstore.Object{Key: "pixels",
		SHA256: pixelRef.SHA256, Bytes: 128,
		VersionID: "v1", ContentType: "application/octet-stream"}
	source := TrainingImageSource{Catalog: store, Objects: store}
	got, err := source.Load(context.Background(), auth.Identity{}, uuid.New(),
		binding, image.Color.ICCSHA256)
	if err != nil || got.Pixels != pixelRef ||
		got.PixelsCount != image.Canvas.Width*image.Canvas.Height {
		t.Fatalf("valid working image not pinned: %v", err)
	}
	if discovered, err := source.DiscoverICC(context.Background(),
		auth.Identity{}, uuid.New(), binding); err != nil ||
		discovered != image.Color.ICCSHA256 {
		t.Fatalf("verified ICC discovery: %q, %v", discovered, err)
	}
	if _, err := source.Load(context.Background(), auth.Identity{}, uuid.New(),
		binding, strings.Repeat("f", 64)); err == nil {
		t.Fatal("wrong ICC lock accepted")
	}
	delete(store.objects, pixelRef.ArtifactID)
	if _, err := source.DiscoverICC(context.Background(), auth.Identity{},
		uuid.New(), binding); err == nil {
		t.Fatal("discovered ICC from image with missing pixels")
	}
	if _, err := source.Load(context.Background(), auth.Identity{}, uuid.New(),
		binding, image.Color.ICCSHA256); err == nil {
		t.Fatal("missing pixel child accepted")
	}
}
