package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type exportDependencyCatalogStub struct {
	objects map[v2input.Entry]objectstore.Object
	reads   map[v2input.Entry]int
	change  v2input.Entry
}

func (s *exportDependencyCatalogStub) LookupExportDocument(ctx context.Context,
	entry v2input.Entry,
) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	value, ok := s.objects[entry]
	if !ok {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	s.reads[entry]++
	if entry == s.change && s.reads[entry] > 1 {
		value.VersionID = "changed"
	}
	return value, nil
}

func TestExportCatalogDependencySemanticsRechecksOriginals(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("source"))}
	binding := v2input.SessionVersionBinding{
		AssetVersionID: uuid.New(), Source: ref, CanvasHash: rawSHA([]byte("canvas")),
	}
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "source": binding, "layers": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := v2input.Entry{Contract: "LayerStack", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: rawSHA(raw),
	}}
	hints, err := v2draft.DocumentChildren(raw, entry.Contract, entry.Ref)
	if err != nil || len(hints) != 1 {
		t.Fatal("layer fixture invalid", err)
	}
	child := v2input.Entry{Ref: ref, Contract: "WorkingImage"}
	objects := map[v2input.Entry]objectstore.Object{
		entry: {Key: "parent", VersionID: "1", SHA256: entry.Ref.SHA256,
			Bytes: int64(len(raw)), ContentType: "application/json"},
		child: {Key: "source", VersionID: "1", SHA256: child.Ref.SHA256,
			Bytes: 10, ContentType: "application/json"},
	}
	newCatalog := func() *exportDependencyCatalogStub {
		return &exportDependencyCatalogStub{objects: objects, reads: map[v2input.Entry]int{}}
	}
	for _, changed := range []v2input.Entry{{}, entry, child} {
		catalog := newCatalog()
		catalog.change = changed
		semantics, err := (ExportCatalogDependencySemanticsFactory{}).BuildExportDependencySemantics(
			context.Background(), catalog)
		if err != nil {
			t.Fatal(err)
		}
		err = semantics.QualifyExportDependency(context.Background(), entry, raw, hints)
		if changed == (v2input.Entry{}) && err != nil {
			t.Fatal("current original rejected", err)
		}
		if changed != (v2input.Entry{}) && err == nil {
			t.Fatal("changed original accepted")
		}
	}
	if err := (ExportCatalogDependencySemantics{Catalog: newCatalog()}).QualifyExportDependency(
		context.Background(), entry, raw, nil); err == nil {
		t.Fatal("missing typed edge accepted")
	}
	if err := (ExportCatalogDependencySemantics{Catalog: newCatalog()}).QualifyExportDependency(
		context.Background(), entry, append(raw, ' '), hints); err == nil {
		t.Fatal("modified original accepted")
	}
}

func TestExportCatalogDependencyChildrenAllowExactRepeatedEdge(t *testing.T) {
	parent := v2input.Entry{Contract: "Params", Ref: v2input.Ref{
		ArtifactID: uuid.New(), SHA256: rawSHA([]byte("parent")),
	}}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("child"))}
	hints := []v2draft.LayerRef{
		{Path: "Document.first", Contract: "WorkingImage", Ref: ref},
		{Path: "Document.second", Contract: "WorkingImage", Ref: ref},
	}
	children, err := uniqueExportDependencyChildren(parent, hints)
	if err != nil || len(children) != 1 {
		t.Fatal("exact repeated edge rejected", err)
	}
	hints[1].Contract = "MaskPixels"
	if children, err := uniqueExportDependencyChildren(parent, hints); err == nil || children != nil {
		t.Fatal("conflicting repeated edge accepted")
	}
	hints[1].Ref = parent.Ref
	if children, err := uniqueExportDependencyChildren(parent, hints); err == nil || children != nil {
		t.Fatal("self edge accepted")
	}
}
