package v2draft

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type RestorationResultDocumentCatalog interface {
	ResolveRestorationDocument(context.Context, RestorationArrayScope, string, v2input.Ref) (objectstore.Object, error)
}

// LoadRestorationResultDocuments reads the root before discovering child refs.
// It repeats every scoped locator after IO, including the source and root.
func LoadRestorationResultDocuments(ctx context.Context, scope RestorationArrayScope,
	catalog RestorationResultDocumentCatalog, objects v2input.SessionAnalysisObjectReader,
	paramsRaw []byte) (RestorationResultDocuments, error) {
	var zero RestorationResultDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || scope.Tenant == uuid.Nil ||
		scope.Task == uuid.Nil || scope.Result == uuid.Nil || !isDocumentRef(v2input.Ref{
		ArtifactID: scope.Root, SHA256: scope.RootSHA}) {
		return zero, v2input.ErrInvalid
	}
	var params struct {
		Base v2input.SessionVersionBinding `json:"base"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil || !isDocumentRef(params.Base.Source) {
		return zero, v2input.ErrInvalid
	}
	rootRef := v2input.Ref{ArtifactID: scope.Root, SHA256: scope.RootSHA}
	type item struct {
		role string
		ref  v2input.Ref
		obj  objectstore.Object
		raw  []byte
	}
	items := []item{{role: "source", ref: params.Base.Source}, {role: "result", ref: rootRef}}
	read := func(index int) error {
		current := &items[index]
		object, err := catalog.ResolveRestorationDocument(ctx, scope, current.role, current.ref)
		if err != nil {
			return err
		}
		if object.Key == "" || object.VersionID == "" || object.VersionID == "null" ||
			object.SHA256 != current.ref.SHA256 {
			return v2input.ErrInvalid
		}
		raw, err := readJSONArtifact(ctx, objects, object, current.ref)
		if err != nil {
			return err
		}
		current.obj, current.raw = object, raw
		return nil
	}
	for i := range items {
		if err := read(i); err != nil {
			return zero, err
		}
	}
	root, err := ParseImageResult(items[1].raw, rootRef)
	if err != nil {
		return zero, err
	}
	items = append(items, item{role: "image", ref: root.WorkingImage},
		item{role: "report", ref: root.Report}, item{role: "changed", ref: root.ChangedMask})
	if root.GeneratedMask != nil {
		items = append(items, item{role: "generated", ref: *root.GeneratedMask})
	}
	for i := 2; i < len(items); i++ {
		if err := read(i); err != nil {
			return zero, err
		}
	}
	for _, current := range items {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		object, err := catalog.ResolveRestorationDocument(ctx, scope, current.role, current.ref)
		if err != nil {
			return zero, err
		}
		if object != current.obj {
			return zero, v2input.ErrExpired
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	docs := RestorationResultDocuments{Source: items[0].raw, Result: items[1].raw,
		Image: items[2].raw, Report: items[3].raw, Changed: items[4].raw}
	if len(items) == 6 {
		docs.Generated = items[5].raw
	}
	return docs, nil
}
