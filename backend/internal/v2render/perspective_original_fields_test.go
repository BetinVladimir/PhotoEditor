package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func originalDenseFixture(t *testing.T, alias bool) ([]byte, v2input.Ref, v2input.Ref) {
	t.Helper()
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	field := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	if alias {
		field = pixels
	}
	canvas := map[string]any{"width": 4, "height": 3, "orientation": 1, "space_id": "original", "source_sha256": strings.Repeat("d", 64)}
	steps := []any{map[string]any{"kind": "dense_warp", "from_canvas": canvas, "to_canvas": canvas, "invertibility": "partial", "valid_region": map[string]any{"x": 0, "y": 0, "width": 4, "height": 3}, "inverse_field": field}}
	hash, err := canonical.Hash(map[string]any{"steps": steps})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"pixels": pixels, "alpha": nil, "canvas": canvas, "color": map[string]any{"profile_id": "linear_srgb_v1", "icc_sha256": strings.Repeat("e", 64), "encoding": "linear"}, "dtype": "float32", "transform_chain": map[string]any{"steps": steps, "hash": hash}})
	if err != nil {
		t.Fatal(err)
	}
	return raw, source, field
}

type perspectiveFieldCatalog struct {
	seen   []v2draft.LayerRef
	object objectstore.Object
	err    error
}

func (c *perspectiveFieldCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	c.seen = append(c.seen, node)
	return c.object, c.err
}

func TestPerspectiveOriginalDenseFieldMembership(t *testing.T) {
	raw, source, field := originalDenseFixture(t, false)
	fields, err := perspectiveOriginalDenseFields(raw, source)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := fields[field.ArtifactID]
	if !ok || len(fields) != 1 || node.Ref != field || node.ParentRef != source || node.Contract != "DenseWarpField" || node.Path != "Document.transform_chain.steps[0].inverse_field" {
		t.Fatalf("incorrect original occurrence: %+v", fields)
	}
	raw, source, _ = originalDenseFixture(t, true)
	if _, err := perspectiveOriginalDenseFields(raw, source); err == nil {
		t.Fatal("pixel/field role alias accepted")
	}
}

func TestPerspectiveOriginalDenseFieldCurrentAuthority(t *testing.T) {
	raw, source, field := originalDenseFixture(t, false)
	fields, err := perspectiveOriginalDenseFields(raw, source)
	if err != nil {
		t.Fatal(err)
	}
	node := fields[field.ArtifactID]
	catalog := &perspectiveFieldCatalog{object: objectstore.Object{Key: "original-field", VersionID: "immutable", Bytes: 224, ContentType: "application/x-npy", SHA256: field.SHA256}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	if _, err := resolvePerspectiveOriginalField(context.Background(), catalog, who, tenant, node); err != nil {
		t.Fatal(err)
	}
	if len(catalog.seen) != 1 || catalog.seen[0].ParentRef != source || catalog.seen[0].Path != node.Path {
		t.Fatal("lost original parent/path")
	}
	catalog.err = v2input.ErrExpired
	if _, err := resolvePerspectiveOriginalField(context.Background(), catalog, who, tenant, node); err != v2input.ErrExpired {
		t.Fatal("cached revoked authority", err)
	}
	catalog.err = nil
	catalog.object.VersionID = "null"
	if _, err := resolvePerspectiveOriginalField(context.Background(), catalog, who, tenant, node); err == nil {
		t.Fatal("unversioned field accepted")
	}
	calls := len(catalog.seen)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolvePerspectiveOriginalField(ctx, catalog, who, tenant, node); err != context.Canceled || len(catalog.seen) != calls {
		t.Fatal("cancelled lookup reached IO")
	}
}
