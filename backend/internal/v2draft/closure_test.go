package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
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

type closureFixture struct {
	objects map[uuid.UUID]objectstore.Object
	bodies  map[string][]byte
}

func (f closureFixture) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID,
	node LayerRef) (objectstore.Object, error) {
	obj, ok := f.objects[node.Ref.ArtifactID]
	if !ok || obj.SHA256 != node.Ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return obj, nil
}

func (f closureFixture) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.bodies[obj.Key])), nil
}

func TestResolveClosurePinsNestedWorkingImagePixels(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(fixture, &images); err != nil || len(images) == 0 {
		t.Fatal(err)
	}
	imageBytes := images[0]
	image, err := review.ParseWorkingImage(imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	imageSHA := sha256.Sum256(imageBytes)
	imageRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(imageSHA[:])}
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: imageRef, CanvasHash: canvasHash}
	styleBytes := []byte(`{}`)
	styleSHA := sha256.Sum256(styleBytes)
	styleRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(styleSHA[:])}
	recipe, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": binding, "base_style": styleRef,
		"session_corrections": nil, "learning_version_id": nil,
		"overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2",
		"parameter_delta": map[string]any{"ev": 0, "log_r": 0, "log_g": 0,
			"log_b": 0, "saturation_delta": 0, "retouch_delta": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	pixel := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	store := closureFixture{objects: map[uuid.UUID]objectstore.Object{
		imageRef.ArtifactID: {Key: "image", SHA256: imageRef.SHA256,
			Bytes: int64(len(imageBytes)), VersionID: "v1", ContentType: "application/json"},
		pixel.ArtifactID: {Key: "pixels", SHA256: pixel.SHA256,
			Bytes: 10, VersionID: "v1", ContentType: "application/octet-stream"},
		styleRef.ArtifactID: {Key: "style", SHA256: styleRef.SHA256,
			Bytes: int64(len(styleBytes)), VersionID: "v1", ContentType: "application/json"},
	}, bodies: map[string][]byte{"image": imageBytes, "style": styleBytes}}
	resolver := ClosureResolver{Catalog: store, Objects: store}
	refs, err := resolver.Resolve(context.Background(), auth.Identity{}, uuid.New(), recipe, binding)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0].ArtifactID != imageRef.ArtifactID.String() ||
		refs[1].ArtifactID != pixel.ArtifactID.String() ||
		refs[2].ArtifactID != styleRef.ArtifactID.String() {
		t.Fatalf("incomplete closure: %+v", refs)
	}
	for i, ref := range refs {
		if ref.Ordinal != i || len(ref.Purpose) != 64 {
			t.Fatalf("invalid SQL ref: %+v", ref)
		}
	}
	protectedPixels := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
	maskBody, err := canonical.Marshal(map[string]any{
		"artifact": protectedPixels, "canvas": image.Canvas,
		"transform_chain": json.RawMessage(image.TransformChain),
		"object_id":       nil, "selector": "manual", "confidence": 1,
		"model_lock_id": nil, "protected": true})
	if err != nil {
		t.Fatal(err)
	}
	maskSum := sha256.Sum256(maskBody)
	maskRef := v2input.Ref{ArtifactID: uuid.New(),
		SHA256: hex.EncodeToString(maskSum[:])}
	store.objects[maskRef.ArtifactID] = objectstore.Object{Key: "mask",
		SHA256: maskRef.SHA256, Bytes: int64(len(maskBody)),
		VersionID: "v1", ContentType: "application/json"}
	store.objects[protectedPixels.ArtifactID] = objectstore.Object{
		Key: "protected", SHA256: protectedPixels.SHA256, Bytes: 64,
		VersionID: "v1", ContentType: "application/octet-stream"}
	store.bodies["mask"] = maskBody
	var recipeFields map[string]any
	if json.Unmarshal(recipe, &recipeFields) != nil {
		t.Fatal("invalid recipe fixture")
	}
	recipeFields["protected_mask"] = maskRef
	pinnedRecipe, err := canonical.Marshal(recipeFields)
	if err != nil {
		t.Fatal(err)
	}
	refs, err = resolver.Resolve(context.Background(), auth.Identity{},
		uuid.New(), pinnedRecipe, binding)
	if err != nil || len(refs) != 5 ||
		refs[3].ArtifactID != maskRef.ArtifactID.String() ||
		refs[4].ArtifactID != protectedPixels.ArtifactID.String() {
		t.Fatalf("protected mask closure invalid: %v %+v", err, refs)
	}
	layerBytes, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "source": binding, "layers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	layerSHA := sha256.Sum256(layerBytes)
	layerRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(layerSHA[:])}
	store.objects[layerRef.ArtifactID] = objectstore.Object{Key: "layers",
		SHA256: layerRef.SHA256, Bytes: int64(len(layerBytes)),
		VersionID: "v1", ContentType: "application/json"}
	store.bodies["layers"] = layerBytes
	layerDeps, err := resolver.ResolveLayer(context.Background(), auth.Identity{}, uuid.New(), layerRef, binding)
	if err != nil || len(layerDeps) != 2 || layerDeps[0].ArtifactID != imageRef.ArtifactID.String() ||
		layerDeps[1].ArtifactID != pixel.ArtifactID.String() || layerDeps[0].Ordinal != 0 {
		t.Fatalf("layer source closure: %+v, %v", layerDeps, err)
	}
	delete(store.objects, pixel.ArtifactID)
	if _, err := resolver.Resolve(context.Background(), auth.Identity{}, uuid.New(), recipe, binding); err == nil {
		t.Fatal("accepted missing pixel artifact")
	}
	store.objects[pixel.ArtifactID] = objectstore.Object{Key: "pixels", SHA256: pixel.SHA256,
		Bytes: 10, VersionID: "v1", ContentType: "application/octet-stream"}
	badPixel := store.objects[pixel.ArtifactID]
	badPixel.ContentType = "application/json"
	store.objects[pixel.ArtifactID] = badPixel
	if _, err := resolver.Resolve(context.Background(), auth.Identity{}, uuid.New(), recipe, binding); err == nil {
		t.Fatal("accepted JSON in WorkingImage pixel slot")
	}
	badPixel.ContentType = "application/octet-stream"
	store.objects[pixel.ArtifactID] = badPixel
	store.bodies["image"] = append([]byte(nil), imageBytes...)
	store.bodies["image"][10] = 'x'
	if _, err := resolver.Resolve(context.Background(), auth.Identity{}, uuid.New(), recipe, binding); err == nil {
		t.Fatal("accepted tampered WorkingImage")
	}
}

func TestLegacyClosureBudgetPreservesOccurrenceAuthorization(t *testing.T) {
	// Many repeated roots exercise per-occurrence scope checks without unbounded IO.
	raw := []byte(`{"fixture":true}`)
	sum := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	fixture := &imageClosureCatalog{closureFixture: closureFixture{objects: map[uuid.UUID]objectstore.Object{ref.ArtifactID: {Key: "root", SHA256: ref.SHA256, Bytes: int64(len(raw)), VersionID: "v1", ContentType: "application/json"}}, bodies: map[string][]byte{"root": raw}}}
	roots := make([]LayerRef, 513)
	for i := range roots {
		roots[i] = LayerRef{Contract: "Artifact", Ref: ref, Path: "root"}
	}
	resolver := ClosureResolver{Catalog: fixture, Objects: fixture}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, v2input.SessionVersionBinding{}); err == nil {
		t.Fatal("legacy occurrence cap bypassed")
	}
	if len(fixture.lookups) != 512+fixture.reads {
		t.Fatal(len(fixture.lookups))
	}
}

type changingClosureCatalog struct {
	closureFixture
	calls    int
	change   func(*objectstore.Object)
	changeAt int
	deny     error
}

func (f *changingClosureCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	obj, err := f.closureFixture.Lookup(ctx, who, tenant, node)
	f.calls++
	at := f.changeAt
	if at == 0 {
		at = 3
	}
	if f.calls == at {
		if f.deny != nil {
			return objectstore.Object{}, f.deny
		}
		if f.change != nil {
			f.change(&obj)
		}
	}
	return obj, err
}

func TestClosurePostReadScopeAndVersionRecheck(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		raw := []byte(`{"child":{"artifact_id":"00000000-0000-4000-8000-000000000001","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
		sum := sha256.Sum256(raw)
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		catalog := &changingClosureCatalog{closureFixture: closureFixture{
			objects: map[uuid.UUID]objectstore.Object{ref.ArtifactID: {Key: "root", VersionID: "v1", SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}},
			bodies:  map[string][]byte{"root": raw},
		}, changeAt: 2, change: func(o *objectstore.Object) { o.VersionID = "v2" }}
		if revoke {
			catalog.deny = v2input.ErrExpired
		}
		resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
		if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), []LayerRef{{Contract: "Artifact", Ref: ref}}, v2input.SessionVersionBinding{}); !errors.Is(err, v2input.ErrExpired) || catalog.calls != 2 {
			t.Fatal("post-read authority drift reached descendant resolution", revoke, err, catalog.calls)
		}
	}
}

func TestClosureRepeatedOccurrenceRequiresIdenticalObjectMetadata(t *testing.T) {
	for name, change := range map[string]func(*objectstore.Object){
		"unchanged": nil,
		"version":   func(o *objectstore.Object) { o.VersionID = "v2" },
		"key":       func(o *objectstore.Object) { o.Key = "other" },
		"bytes":     func(o *objectstore.Object) { o.Bytes++ },
		"media":     func(o *objectstore.Object) { o.ContentType = "application/octet-stream" },
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(`{"fixture":true}`)
			sum := sha256.Sum256(raw)
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			catalog := &changingClosureCatalog{closureFixture: closureFixture{
				objects: map[uuid.UUID]objectstore.Object{ref.ArtifactID: {Key: "root", VersionID: "v1", SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}},
				bodies:  map[string][]byte{"root": raw},
			}, change: change}
			resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
			refs, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), []LayerRef{
				{Contract: "Artifact", Ref: ref, Path: "first"},
				{Contract: "Artifact", Ref: ref, Path: "second"},
			}, v2input.SessionVersionBinding{})
			if catalog.calls != 3 {
				t.Fatal("repeated occurrence was not scoped independently", catalog.calls)
			}
			if change == nil {
				if err != nil || len(refs) != 2 {
					t.Fatal("stable repeated object refused", err)
				}
			} else if err != v2input.ErrExpired {
				t.Fatal("changed immutable metadata reused", err)
			}
		})
	}
}

type cancelingClosureReader struct {
	closureFixture
	cancel context.CancelFunc
	opened bool
	closed bool
}

type closureCancelReadCloser struct {
	*bytes.Reader
	owner *cancelingClosureReader
}

func (r closureCancelReadCloser) Read(p []byte) (int, error) {
	r.owner.cancel()
	return r.Reader.Read(p)
}

func (r closureCancelReadCloser) Close() error { r.owner.closed = true; return nil }

func (f *cancelingClosureReader) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	f.opened = true
	return closureCancelReadCloser{bytes.NewReader(f.bodies[obj.Key]), f}, nil
}

func TestClosureCancellationIsNotInvalidInput(t *testing.T) {
	raw := []byte(`{"fixture":true}`)
	sum := sha256.Sum256(raw)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	obj := objectstore.Object{Key: "root", VersionID: "v1", SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
	fixture := closureFixture{objects: map[uuid.UUID]objectstore.Object{ref.ArtifactID: obj}, bodies: map[string][]byte{"root": raw}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelingClosureReader{closureFixture: fixture, cancel: cancel}
	if _, err := readJSONArtifact(ctx, reader, obj, ref); !errors.Is(err, context.Canceled) || !reader.opened || !reader.closed {
		t.Fatal("in-flight cancellation lost or reader leaked", err, reader.opened, reader.closed)
	}
	catalog := &changingClosureCatalog{closureFixture: fixture}
	resolver := ClosureResolver{Catalog: catalog, Objects: fixture}
	if _, err := resolver.resolveRoots(ctx, auth.Identity{}, uuid.New(), []LayerRef{{Contract: "Artifact", Ref: ref}}, v2input.SessionVersionBinding{}); !errors.Is(err, context.Canceled) || catalog.calls != 0 {
		t.Fatal("canceled traversal performed catalog IO", err, catalog.calls)
	}
	reader.opened = false
	if _, err := readJSONArtifact(ctx, reader, obj, ref); !errors.Is(err, context.Canceled) || reader.opened {
		t.Fatal("canceled document read opened an object", err)
	}
}

type closureBoundedReader struct {
	consumed int
	closed   bool
}

func (r *closureBoundedReader) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return r, nil
}
func (r *closureBoundedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.consumed += len(p)
	return len(p), nil
}
func (r *closureBoundedReader) Close() error { r.closed = true; return nil }

func TestClosureJSONReadRejectsOversizedStreamAtDeclaredBoundary(t *testing.T) {
	for _, size := range []int64{2, 4096, maxDocumentJSON} {
		reader := &closureBoundedReader{}
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
		obj := objectstore.Object{Key: "root", VersionID: "v1", SHA256: ref.SHA256, Bytes: size, ContentType: "application/json"}
		if _, err := readJSONArtifact(context.Background(), reader, obj, ref); !errors.Is(err, v2input.ErrInvalid) || reader.consumed != int(size+1) || !reader.closed {
			t.Fatal("stream exceeded declared size or reader leaked", size, err, reader.consumed, reader.closed)
		}
	}
}
