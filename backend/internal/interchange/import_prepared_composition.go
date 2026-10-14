package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// The prepared closure proves each dependency separately. This additional
// gate proves that the ordered replacements form a valid production StyleV2.
type ImportPreparedCompositionQualifier struct {
	Inner    ImportPreparedRecipeQualifier
	Scope    ImportPreparedScope
	Resolver ImportClosureNodeResolver
	Masters  ImportMasterReader
	Native   *NativeExportStyleValidator
}

func (q ImportPreparedCompositionQualifier) QualifyPreparedImportRecipe(ctx context.Context, ref v2input.Ref, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Inner == nil || q.Scope.Binding != binding || q.Scope.Tenant == uuid.Nil || q.Scope.Workflow == uuid.Nil || q.Scope.Entry == uuid.Nil || !validImportRef(ref) || rawSHA(raw) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	if err := q.Inner.QualifyPreparedImportRecipe(ctx, ref, append([]byte(nil), owned...), binding); err != nil {
		return err
	}
	return q.qualifyComposition(ctx, ref, owned, binding)
}

func (q ImportPreparedCompositionQualifier) qualifyComposition(ctx context.Context, ref v2input.Ref, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Resolver == nil || q.Masters == nil || q.Native == nil || q.Scope.Binding != binding || q.Native.contract != "RenderComposition" || !validImportRef(ref) || rawSHA(raw) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	if err := v2input.ValidateEditingRecipeV3(body, binding); err != nil {
		return err
	}
	var recipe struct {
		Base      v2input.Ref   `json:"base_style"`
		Overrides []v2input.Ref `json:"overrides"`
	}
	if err := json.Unmarshal(body, &recipe); err != nil {
		return err
	}
	if !validImportRef(recipe.Base) || len(recipe.Overrides) > 128 {
		return v2input.ErrInvalid
	}
	entries := make([]v2input.Entry, 0, len(recipe.Overrides)+1)
	entries = append(entries, v2input.Entry{Ref: recipe.Base, Contract: "StyleV2"})
	for _, override := range recipe.Overrides {
		if !validImportRef(override) {
			return v2input.ErrInvalid
		}
		entries = append(entries, v2input.Entry{Ref: override, Contract: "Override"})
	}
	readSource := func() (exportCompositionDocument, error) {
		var zero exportCompositionDocument
		current, err := q.Masters.ReadImportMaster(ctx, binding.Source, 1<<20)
		if err != nil {
			return zero, err
		}
		owned := append([]byte(nil), current...)
		if len(owned) < 2 || len(owned) > 8<<20 || rawSHA(owned) != binding.Source.SHA256 {
			return zero, v2input.ErrInvalid
		}
		normalized, err := canonical.Bytes(owned)
		if err != nil || !bytes.Equal(normalized, owned) {
			return zero, v2input.ErrInvalid
		}
		return exportCompositionDocument{SHA: binding.Source.SHA256, Body: owned}, ctx.Err()
	}
	readDependency := func(entry v2input.Entry) (exportCompositionDocument, error) {
		var zero exportCompositionDocument
		node, err := q.Resolver.ResolveImportNode(ctx, entry)
		if err != nil {
			return zero, err
		}
		return ownExportCompositionDocument(ctx, ExportClosureNode{Entry: node.Entry, Body: node.Body}, entry)
	}
	source, err := readSource()
	if err != nil {
		return err
	}
	bundle := exportCompositionBundle{Schema: 1, Source: source, Overrides: []exportCompositionDocument{}}
	remaining := int64(128<<20) - int64(len(source.Body))
	for i, entry := range entries {
		doc, err := readDependency(entry)
		if err != nil {
			return err
		}
		remaining -= int64(len(doc.Body))
		if remaining < 0 {
			return v2input.ErrInvalid
		}
		if i == 0 {
			bundle.Base = doc
		} else {
			bundle.Overrides = append(bundle.Overrides, doc)
		}
	}
	encoded, err := canonical.Marshal(bundle)
	if err != nil || len(encoded) > 128<<20 {
		return v2input.ErrInvalid
	}
	probe := v2input.Entry{Ref: v2input.Ref{ArtifactID: ref.ArtifactID, SHA256: rawSHA(encoded)}, Contract: "RenderComposition"}
	if _, err := q.Native.ExportDocumentChildren(ctx, probe, encoded); err != nil {
		return err
	}
	current, err := readSource()
	if err != nil {
		return err
	}
	if !bytes.Equal(current.Body, source.Body) {
		return v2input.ErrInvalid
	}
	for i, entry := range entries {
		current, err := readDependency(entry)
		if err != nil {
			return err
		}
		expected := bundle.Base
		if i > 0 {
			expected = bundle.Overrides[i-1]
		}
		if current.SHA != expected.SHA || !bytes.Equal(current.Body, expected.Body) {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

var _ ImportPreparedRecipeQualifier = ImportPreparedCompositionQualifier{}
