package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type regionScopeFixture struct {
	closureFixture
	calls []LayerRef
}

func (f *regionScopeFixture) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	f.calls = append(f.calls, node)
	return f.closureFixture.Lookup(ctx, who, tenant, node)
}
func TestRegionResolverExpandsScopedStyleAndSource(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil {
		t.Fatal("images")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	imageRaw, _ := canonical.Marshal(image)
	imageHash, _ := canonical.Hash(image)
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: imageHash}
	canvasHash, _ := canonical.Hash(image.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvasHash}
	lut := v2input.Ref{ArtifactID: uuid.New(), SHA256: image.Pixels.SHA256}
	styleDoc := map[string]any{"lut": lut, "schema_version": "2.0.0", "renderer_version": "photo_renderer_v2", "global": map[string]any{"gains_rgb": []float64{1, 1, 1}, "saturation": 1}, "beauty": map[string]any{"enabled": false, "smoothing": 0}}
	styleBody, _ := canonical.Marshal(styleDoc)
	styleHash, _ := canonical.Hash(styleDoc)
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: styleHash}
	pixel := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	f := &regionScopeFixture{closureFixture: closureFixture{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{"image": imageRaw, "style": styleBody}}}
	for ref, key := range map[v2input.Ref]string{source: "image", style: "style", pixel: "pixels", lut: "lut"} {
		obj := objectstore.Object{Key: key, SHA256: ref.SHA256, VersionID: "pinned", ContentType: "application/octet-stream", Bytes: 12}
		if raw, ok := f.bodies[key]; ok {
			obj.ContentType = "application/json"
			obj.Bytes = int64(len(raw))
		}
		f.objects[ref.ArtifactID] = obj
	}
	resolver := ClosureResolver{Catalog: f, Objects: f}
	entries, err := resolver.ResolveRegionInputs(context.Background(), auth.Identity{}, uuid.New(), binding, style, CorrectionVector{})
	if err != nil || len(entries) != 4 {
		t.Fatalf("closure %v %v", entries, err)
	}
	parentChecked := false
	for _, node := range f.calls {
		if node.Ref == lut {
			parentChecked = node.ParentRef == style && node.Contract == "Artifact"
		}
	}
	if !parentChecked {
		t.Fatal("style child lost parent provenance")
	}
	if _, err = resolver.ResolveRegionInputs(context.Background(), auth.Identity{}, uuid.New(), binding, style, CorrectionVector{EV: 3}); err == nil {
		t.Fatal("out-of-bounds effective style accepted")
	}
	delete(f.objects, lut.ArtifactID)
	if _, err = resolver.ResolveRegionInputs(context.Background(), auth.Identity{}, uuid.New(), binding, style, CorrectionVector{}); err == nil {
		t.Fatal("missing scoped LUT admitted")
	}
	// Immutable JSON bytes must still match the declared content digest.
	f.bodies["style"] = append(styleBody, ' ')
	if _, err = resolver.ResolveRegionInputs(context.Background(), auth.Identity{}, uuid.New(), binding, style, CorrectionVector{}); err == nil {
		t.Fatal("changed style bytes admitted")
	}
}
