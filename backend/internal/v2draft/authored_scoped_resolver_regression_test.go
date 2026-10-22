package v2draft

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Boundary catalog doubles select scope only; every assertion below executes
// the real ClosureResolver, canonical object IO and real typed parsers. These
// tests do not establish the SQLAuthoredSegmentCatalog's DB authority.
type authoredScopedRegressionCatalog struct {
	closureFixture
	bindings         map[v2input.Ref]v2input.SessionVersionBinding
	parents          map[v2input.Ref]v2input.Ref
	geometries       map[v2input.Ref]DocumentGeometry
	calls            int
	alternateBinding *v2input.SessionVersionBinding
}

func (s *authoredScopedRegressionCatalog) LayerStackBinding(_ context.Context, _ auth.Identity, _ uuid.UUID, node LayerRef) (v2input.SessionVersionBinding, bool, error) {
	s.calls++
	selected, ok := s.bindings[node.Ref]
	if !ok {
		return v2input.SessionVersionBinding{}, false, nil
	}
	if node.ParentRef != s.parents[node.Ref] {
		return v2input.SessionVersionBinding{}, false, v2input.ErrInvalid
	}
	if s.alternateBinding != nil && s.calls > 1 {
		return *s.alternateBinding, true, nil
	}
	return selected, true, nil
}
func (s *authoredScopedRegressionCatalog) Geometry(_ context.Context, _ auth.Identity, _ uuid.UUID, node LayerRef) (DocumentGeometry, bool, error) {
	geometry, ok := s.geometries[node.Ref]
	return geometry, ok, nil
}
func authoredScopedRegressionFixture(t *testing.T) (*authoredScopedRegressionCatalog, v2input.SessionVersionBinding, v2input.SessionVersionBinding, v2input.Ref) {
	t.Helper()
	data, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(data, &images) != nil || len(images) == 0 {
		t.Fatal("image fixture")
	}
	original, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	store := closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	add := func(raw []byte) v2input.Ref {
		digest, e := canonical.Hash(json.RawMessage(raw))
		if e != nil {
			t.Fatal(e)
		}
		ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}
		key := ref.ArtifactID.String()
		store.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: digest, Bytes: int64(len(raw)), VersionID: "actual-test-version", ContentType: "application/json"}
		store.bodies[key] = raw
		return ref
	}
	originalRef := add(images[0])
	canvas, e := canonical.Hash(original.Canvas)
	if e != nil {
		t.Fatal(e)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: originalRef, CanvasHash: canvas}
	// Same original source lineage, distinct actual current descriptor and canvas.
	current, e := review.ParseWorkingImage(images[3])
	if e != nil {
		t.Fatal(e)
	}
	current.Pixels.ID = uuid.New()
	raw, e := canonical.Marshal(current)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = review.ParseWorkingImage(raw); e != nil {
		t.Fatal(e)
	}
	currentRef := add(raw)
	currentCanvas, e := canonical.Hash(current.Canvas)
	if e != nil {
		t.Fatal(e)
	}
	selected := v2input.SessionVersionBinding{AssetVersionID: binding.AssetVersionID, Source: currentRef, CanvasHash: currentCanvas}
	for _, image := range []review.WorkingImage{original, current} {
		store.objects[image.Pixels.ID] = objectstore.Object{Key: image.Pixels.ID.String(), SHA256: image.Pixels.SHA256, Bytes: 8, VersionID: "actual-test-version", ContentType: "application/octet-stream"}
		if image.Alpha != nil {
			store.objects[image.Alpha.ID] = objectstore.Object{Key: image.Alpha.ID.String(), SHA256: image.Alpha.SHA256, Bytes: 8, VersionID: "actual-test-version", ContentType: "application/octet-stream"}
		}
	}
	stackRaw, e := canonical.Marshal(map[string]any{"schema_version": 1, "source": selected, "layers": []any{}})
	if e != nil {
		t.Fatal(e)
	}
	stack := add(stackRaw)
	chain, e := canonical.Hash(current.TransformChain)
	if e != nil {
		t.Fatal(e)
	}
	catalog := &authoredScopedRegressionCatalog{closureFixture: store, bindings: map[v2input.Ref]v2input.SessionVersionBinding{stack: selected}, parents: map[v2input.Ref]v2input.Ref{}, geometries: map[v2input.Ref]DocumentGeometry{currentRef: {CanvasHash: currentCanvas, TransformHash: chain}}}
	return catalog, binding, selected, stack
}
func TestAuthoredScopedResolverOriginalBootstrapAndCurrentStack(t *testing.T) {
	catalog, original, current, stack := authoredScopedRegressionFixture(t)
	resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
	roots := []LayerRef{{Path: "original", Contract: "WorkingImage", Ref: original.Source, CanvasHash: original.CanvasHash}, {Path: "current", Contract: "LayerStack", Ref: stack}}
	evidence, err := resolver.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, original)
	if err != nil {
		t.Fatal("lawful scoped current stack", err)
	}
	foundOriginal, foundCurrent := false, false
	for _, entry := range evidence.Entries {
		if entry.Ref == original.Source {
			foundOriginal = true
		}
		if entry.Ref == current.Source {
			foundCurrent = true
		}
	}
	if !foundOriginal || !foundCurrent || catalog.calls == 0 {
		t.Fatal("actual original/current traversal missing")
	}
}
func TestAuthoredScopedResolverForeignParentAndOrdinaryBindingRefusal(t *testing.T) {
	catalog, original, _, stack := authoredScopedRegressionFixture(t)
	roots := []LayerRef{{Path: "original", Contract: "WorkingImage", Ref: original.Source, CanvasHash: original.CanvasHash}, {Path: "current", Contract: "LayerStack", Ref: stack, ParentRef: v2input.Ref{ArtifactID: uuid.New()}}}
	resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
	if _, err := resolver.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("foreign parent accepted", err)
	}
	// Existing ordinary catalog must not learn a broad current-source fallback.
	ordinary := ClosureResolver{Catalog: catalog.closureFixture, Objects: catalog}
	roots[1].ParentRef = v2input.Ref{}
	if _, err := ordinary.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("ordinary source guard weakened", err)
	}
}
func TestAuthoredScopedResolverGeometryBootstrapMustRemainOrdinary(t *testing.T) {
	catalog, original, _, _ := authoredScopedRegressionFixture(t)
	raw := catalog.bodies[catalog.objects[original.Source.ArtifactID].Key]
	image, e := review.ParseWorkingImage(raw)
	if e != nil {
		t.Fatal(e)
	}
	chain, e := canonical.Hash(image.TransformChain)
	if e != nil {
		t.Fatal(e)
	}
	catalog.geometries[original.Source] = DocumentGeometry{CanvasHash: original.CanvasHash, TransformHash: chain}
	resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
	if _, err := resolver.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), []LayerRef{{Path: "original", Contract: "WorkingImage", Ref: original.Source, CanvasHash: original.CanvasHash}}, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("bootstrap source proof silently invented", err)
	}
}

func TestAuthoredScopedResolverRepeatedStackRechecksDifferentParent(t *testing.T) {
	catalog, original, _, stack := authoredScopedRegressionFixture(t)
	resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
	roots := []LayerRef{{Path: "original", Contract: "WorkingImage", Ref: original.Source, CanvasHash: original.CanvasHash}, {Path: "first", Contract: "LayerStack", Ref: stack}, {Path: "foreign-repeat", Contract: "LayerStack", Ref: stack, ParentRef: v2input.Ref{ArtifactID: uuid.New()}}}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("cached layer bytes bypassed occurrence binding", err)
	}
	if catalog.calls != 2 {
		t.Fatal("both parent occurrences must recheck binding", catalog.calls)
	}
}
func TestAuthoredScopedResolverTransformedMaskNeedsExactGeometry(t *testing.T) {
	catalog, original, current, _ := authoredScopedRegressionFixture(t)
	image, err := review.ParseWorkingImage(catalog.bodies[catalog.objects[current.Source.ArtifactID].Key])
	if err != nil {
		t.Fatal(err)
	}
	mask := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: current.Source.SHA256}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: 1, Protected: true}
	raw, err := canonical.Marshal(mask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = review.ParseMaskRef(raw); err != nil {
		t.Fatal(err)
	}
	digest, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}
	key := ref.ArtifactID.String()
	catalog.objects[ref.ArtifactID] = objectstore.Object{Key: key, SHA256: digest, Bytes: int64(len(raw)), VersionID: "actual-test-version", ContentType: "application/json"}
	catalog.bodies[key] = raw
	catalog.objects[mask.Artifact.ID] = objectstore.Object{Key: mask.Artifact.ID.String(), SHA256: mask.Artifact.SHA256, Bytes: 8, VersionID: "actual-test-version", ContentType: "application/octet-stream"}
	roots := []LayerRef{{Path: "original", Contract: "WorkingImage", Ref: original.Source, CanvasHash: original.CanvasHash}, {Path: "mask", Contract: "MaskRef", Ref: ref}}
	resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
	if _, err = resolver.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("current mask must not fall back to original canvas", err)
	}
	catalog.geometries[ref] = catalog.geometries[current.Source]
	if _, err = resolver.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, original); err != nil {
		t.Fatal("exact scoped transformed mask refused", err)
	}
	wrong := catalog.geometries[ref]
	wrong.CanvasHash = original.CanvasHash
	catalog.geometries[ref] = wrong
	if _, err = resolver.ResolveBoundProvenanceEvidence(context.Background(), auth.Identity{}, uuid.New(), roots, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("wrong mask canvas accepted", err)
	}
}

// A successful second authority lookup may select a valid, different binding.
// Cached bytes must still be validated against THAT occurrence's exact source.
func TestAuthoredScopedResolverRepeatedStackValidAlternateBindingRefused(t *testing.T) {
	catalog, original, _, stack := authoredScopedRegressionFixture(t)
	resolver := ClosureResolver{Catalog: catalog, Objects: catalog}
	roots := []LayerRef{{Path: "original", Contract: "WorkingImage", Ref: original.Source, CanvasHash: original.CanvasHash}, {Path: "first", Contract: "LayerStack", Ref: stack}, {Path: "second", Contract: "LayerStack", Ref: stack}}
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, original); err != nil {
		t.Fatal("same binding repeated occurrence refused", err)
	}
	if catalog.calls != 2 {
		t.Fatal("same binding occurrences not both validated", catalog.calls)
	}
	catalog.calls = 0
	catalog.alternateBinding = &original
	if _, err := resolver.resolveRoots(context.Background(), auth.Identity{}, uuid.New(), roots, original); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("cached stack accepted valid but different occurrence binding", err)
	}
	if catalog.calls != 2 {
		t.Fatal("both selected bindings must be checked", catalog.calls)
	}
}
