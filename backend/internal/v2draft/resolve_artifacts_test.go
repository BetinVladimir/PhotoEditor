package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestReadResolvedScalarsPinsStyleAndSessionPlan(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(fixture, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	imageRef := ref(images[0])
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: imageRef, CanvasHash: canvasHash}
	styleBytes, err := os.ReadFile("../run/identity_style.json")
	if err != nil {
		t.Fatal(err)
	}
	styleBytes, err = canonical.Bytes(styleBytes)
	if err != nil {
		t.Fatal(err)
	}
	styleRef := ref(styleBytes)
	plan, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "snapshot_digest": testDigest('a'),
		"items": []any{map[string]any{"binding": binding, "ordinal": 0,
			"delta": CorrectionVector{EV: .2}, "disposition": "apply", "reason": nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	planRef := ref(plan)
	recipe, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": binding, "base_style": styleRef,
		"session_corrections": planRef, "learning_version_id": nil,
		"overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2",
		"parameter_delta": CorrectionVector{},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	put := func(ref v2input.Ref, key string, body []byte, contentType string) {
		store.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: ref.SHA256,
			Bytes: int64(len(body)), VersionID: "v1", ContentType: contentType}
		store.bodies[key] = body
	}
	put(imageRef, "image", images[0], "application/json")
	put(v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256},
		"pixels", []byte("pixel-data"), "application/octet-stream")
	put(styleRef, "style", styleBytes, "application/json")
	put(planRef, "plan", plan, "application/json")
	resolver := ClosureResolver{Catalog: store, Objects: store}
	style, gotRef, session, err := resolver.ReadResolvedScalars(context.Background(),
		auth.Identity{}, uuid.New(), recipe, binding)
	if err != nil || gotRef != styleRef || session == nil || session.Ref != planRef ||
		session.Vector.EV != .2 || style.GainsRGB != [3]float64{1, 1, 1} {
		t.Fatal("resolved scalars", style, gotRef, session, err)
	}
	delete(store.objects, styleRef.ArtifactID)
	if _, _, _, err = resolver.ReadResolvedScalars(context.Background(), auth.Identity{},
		uuid.New(), recipe, binding); err == nil {
		t.Fatal("accepted missing StyleV2")
	}
}
