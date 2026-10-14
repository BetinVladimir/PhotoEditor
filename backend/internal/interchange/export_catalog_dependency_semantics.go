package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ExportDependencySemanticsFactory binds a process-installed policy to the
// exact foreground request or scheduler claim catalogue.
type ExportDependencySemanticsFactory interface {
	BuildExportDependencySemantics(context.Context, ExportDocumentCatalog) (ExportDependencySemantics, error)
}

type ExportCatalogDependencySemanticsFactory struct{}

func (ExportCatalogDependencySemanticsFactory) BuildExportDependencySemantics(ctx context.Context,
	catalog ExportDocumentCatalog,
) (ExportDependencySemantics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, v2input.ErrInvalid
	}
	return ExportCatalogDependencySemantics{Catalog: catalog}, nil
}

// ExportCatalogDependencySemantics proves that every original dependency and
// typed child remains in the current export catalogue. Contract-specific
// source, model, operation and consent checks are layered above this policy.
type ExportCatalogDependencySemantics struct {
	Catalog ExportDocumentCatalog
}

func (s ExportCatalogDependencySemantics) QualifyExportDependency(ctx context.Context,
	entry v2input.Entry, raw []byte, hints []v2draft.LayerRef,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Catalog == nil || !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 || len(hints) > v2input.MaxEntries {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) {
		return v2input.ErrInvalid
	}
	if entry.Contract == "Params" {
		operation, err := exportParamsOperationShape(body)
		if err != nil || operation == "" {
			return v2input.ErrInvalid
		}
	}
	expected, err := v2draft.DocumentChildren(body, entry.Contract, entry.Ref)
	if err != nil {
		return v2input.ErrInvalid
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return v2input.ErrInvalid
	}
	hintsJSON, err := json.Marshal(hints)
	if err != nil || !bytes.Equal(expectedJSON, hintsJSON) {
		return v2input.ErrInvalid
	}
	check := func(item v2input.Entry) (objectstore.Object, error) {
		object, err := s.Catalog.LookupExportDocument(ctx, item)
		if err != nil {
			return objectstore.Object{}, err
		}
		if object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
			object.SHA256 != item.Ref.SHA256 || object.Bytes < 1 {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		return object, nil
	}
	parent, err := check(entry)
	if err != nil || parent.Bytes != int64(len(body)) || parent.ContentType != "application/json" {
		return v2input.ErrInvalid
	}
	children := make([]v2input.Entry, 0, len(hints))
	objects := make([]objectstore.Object, 0, len(hints))
	seen := map[string]bool{}
	for _, hint := range hints {
		child := v2input.Entry{Ref: hint.Ref, Contract: hint.Contract}
		if !validImportRef(child.Ref) || child.Contract == "" || child.Ref.ArtifactID == entry.Ref.ArtifactID ||
			seen[child.Ref.ArtifactID.String()] {
			return v2input.ErrInvalid
		}
		seen[child.Ref.ArtifactID.String()] = true
		object, err := check(child)
		if err != nil {
			return err
		}
		children = append(children, child)
		objects = append(objects, object)
	}
	current, err := check(entry)
	if err != nil || current != parent {
		return v2input.ErrInvalid
	}
	for index, child := range children {
		current, err := check(child)
		if err != nil || current != objects[index] {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

var _ ExportDependencySemanticsFactory = ExportCatalogDependencySemanticsFactory{}
var _ ExportDependencySemantics = ExportCatalogDependencySemantics{}
