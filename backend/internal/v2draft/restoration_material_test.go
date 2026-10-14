package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type restorationCatalog struct {
	objects map[uuid.UUID]objectstore.Object
	calls   map[uuid.UUID]int
	change  uuid.UUID
}

func (c *restorationCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID,
	node LayerRef) (objectstore.Object, error) {
	c.calls[node.Ref.ArtifactID]++
	obj, ok := c.objects[node.Ref.ArtifactID]
	if !ok {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.Ref.ArtifactID == c.change && c.calls[node.Ref.ArtifactID] > 1 {
		obj.VersionID = "changed"
	}
	return obj, nil
}

type restorationObjects map[string][]byte

func (o restorationObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o[obj.Key])), nil
}

func TestRestorationLoaderPinsAndRechecksAllDocumentVersions(t *testing.T) {
	sha := func(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
	body := func(value string) []byte {
		raw, err := canonical.Marshal(map[string]any{"value": value})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	sourceBody, targetBody, guardBody := body("source"), body("target"), body("guard")
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha(sourceBody)}
	target := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha(targetBody)}
	guard := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha(guardBody)}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: sha([]byte("canvas"))}
	params, err := canonical.Marshal(map[string]any{"base": base, "mask": target, "protected": guard})
	if err != nil {
		t.Fatal(err)
	}
	store := restorationObjects{}
	objects := map[uuid.UUID]objectstore.Object{}
	for ref, raw := range map[v2input.Ref][]byte{source: sourceBody, target: targetBody, guard: guardBody} {
		key := ref.ArtifactID.String()
		store[key] = raw
		objects[ref.ArtifactID] = objectstore.Object{Key: key, VersionID: "v1",
			SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
	}
	catalog := &restorationCatalog{objects: objects, calls: map[uuid.UUID]int{}}
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha([]byte("parent"))}
	scopes := RestorationManualScopes{root: parent,
		original: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha([]byte("original"))},
		verified: true,
		target: LayerRef{Contract: "MaskRef", Ref: target,
			CanvasHash: base.CanvasHash, ExpectedBinding: &base},
		protected: LayerRef{Contract: "MaskRef", Ref: guard,
			CanvasHash: base.CanvasHash, ExpectedBinding: &base}}
	loader, err := NewRestorationManualMaterialLoader(catalog, nil, store, scopes)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if _, err := NewRestorationManualMaterialLoader(catalog, nil, store,
		RestorationManualScopes{root: scopes.root, original: scopes.original,
			target: scopes.target, protected: scopes.protected}); err == nil {
		t.Fatal("unverified origin scopes accepted")
	}
	if _, err := (RestorationMaterialLoader{BaseCatalog: catalog, Objects: store}).Load(
		context.Background(), who, uuid.New(), "image.reflection", params); err == nil {
		t.Fatal("direct unverified loader accepted")
	}
	docs, err := loader.Load(context.Background(), who, uuid.New(), "image.reflection", params)
	if err != nil || !bytes.Equal(docs.Source, sourceBody) ||
		!bytes.Equal(docs.Target, targetBody) || !bytes.Equal(docs.Protected, guardBody) {
		t.Fatal("exact versioned documents not loaded", err)
	}
	for _, ref := range []v2input.Ref{source, target, guard} {
		if catalog.calls[ref.ArtifactID] != 2 {
			t.Fatal("scope not rechecked", ref)
		}
	}
	catalog.calls = map[uuid.UUID]int{}
	catalog.change = guard.ArtifactID
	if _, err = loader.Load(context.Background(), who, uuid.New(), "image.reflection", params); err == nil {
		t.Fatal("changed object version accepted")
	}
}
