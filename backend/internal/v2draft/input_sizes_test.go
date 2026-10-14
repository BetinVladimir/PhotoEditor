package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type sizeCatalog struct {
	calls int
	drift bool
	fail  bool
}

func (c *sizeCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node LayerRef) (objectstore.Object, error) {
	c.calls++
	if c.fail {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	o := objectstore.Object{Key: node.Ref.ArtifactID.String(), VersionID: "v1", SHA256: node.Ref.SHA256, Bytes: 123}
	if c.drift && c.calls > 2 {
		o.VersionID = "v2"
	}
	return o, nil
}
func TestInputSizesRechecksCatalogAndRejectsPartial(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	nodes := []LayerRef{{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "WorkingImage"}, {Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Contract: "WorkingPixels"}}
	c := &sizeCatalog{}
	sizes, err := ReadInputSizes(context.Background(), c, who, tenant, nodes)
	if err != nil || len(sizes) != 2 || sizes[0].Bytes != 123 || c.calls != 4 {
		t.Fatal("exact sizes", err, c.calls)
	}
	c = &sizeCatalog{drift: true}
	if sizes, err := ReadInputSizes(context.Background(), c, who, tenant, nodes); err != v2input.ErrConflict || sizes != nil {
		t.Fatal("catalog drift retained sizes", err)
	}
	c = &sizeCatalog{fail: true}
	if sizes, err := ReadInputSizes(context.Background(), c, who, tenant, nodes); err != v2input.ErrInvalid || sizes != nil {
		t.Fatal("failed lookup retained sizes", err)
	}
	c = &sizeCatalog{}
	if sizes, err := ReadInputSizes(context.Background(), c, who, tenant, []LayerRef{nodes[0], nodes[0]}); err != v2input.ErrInvalid || sizes != nil || c.calls != 0 {
		t.Fatal("duplicate reached catalog", err)
	}
}

func TestInputEvidenceRetainsExactLocations(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	catalog := &sizeCatalog{}
	got, err := ReadInputEvidence(context.Background(), catalog, who, uuid.New(), []LayerRef{{Ref: ref, Contract: "Params"}})
	if err != nil || len(got) != 1 || got[0].Ref != ref || got[0].Contract != "Params" || got[0].Object.Key != ref.ArtifactID.String() || got[0].Object.VersionID != "v1" || got[0].Object.SHA256 != ref.SHA256 || got[0].Object.Bytes != 123 || catalog.calls != 2 {
		t.Fatal("lost original catalog evidence", err)
	}
}
