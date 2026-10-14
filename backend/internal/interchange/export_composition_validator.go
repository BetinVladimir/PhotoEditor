package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type exportCompositionDocument struct {
	SHA  string          `json:"sha256"`
	Body json.RawMessage `json:"body"`
}
type exportCompositionBundle struct {
	Schema    int                         `json:"schema_version"`
	Source    exportCompositionDocument   `json:"source"`
	Base      exportCompositionDocument   `json:"base"`
	Overrides []exportCompositionDocument `json:"overrides"`
}

// This private validator is only a staging reader, never a graph authority.
// The complete bundle schema is qualified by the fixed native probe; the graph
// separately qualifies all typed dependencies through Base.
type exportCompositionStagingValidator struct{}

func (exportCompositionStagingValidator) ExportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch entry.Contract {
	case "StyleV2", "Override", "WorkingImage", "RenderComposition", "ResolvedOrigin", "OperationParams", "SeriesOrigin", "SeriesMeasureOrigin", "MaskOrigin", "BodyOrigin", "BodyDependencyOrigin", "BodyFitOrigin", "PairAlignmentOrigin", "FaceDetectorOrigin":
	default:
		return nil, v2input.ErrInvalid
	}
	if !validImportRef(entry.Ref) || rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	return nil, nil
}

type ExportFrozenCompositionValidator struct {
	base   ExportTypedDocumentValidator
	reader ExportClosureNodeResolver
	native *NativeExportStyleValidator
}

func newExportFrozenCompositionValidator(base ExportTypedDocumentValidator, reader ExportClosureNodeResolver, scratch *ExportArchiveScratch, probe ExportNativeProbe, models map[string]string, runtime string) (*ExportFrozenCompositionValidator, error) {
	return newExportFrozenCompositionValidatorWithPolicies(base, reader, scratch, probe, models, models, runtime)
}

func newExportFrozenCompositionValidatorWithPolicies(base ExportTypedDocumentValidator, reader ExportClosureNodeResolver, scratch *ExportArchiveScratch, probe ExportNativeProbe, styleModels, operationModels map[string]string, runtime string) (*ExportFrozenCompositionValidator, error) {
	if base == nil || reader == nil {
		return nil, v2input.ErrInvalid
	}
	native, err := NewNativeExportStyleValidator(scratch, probe, styleModels, runtime, exportCompositionStagingValidator{})
	if err != nil {
		return nil, err
	}
	if operationModels == nil || len(operationModels) > 1024 {
		return nil, v2input.ErrInvalid
	}
	for key, value := range operationModels {
		if len(key) < 1 || len(key) > 240 || len(value) < 1 || len(value) > 240 {
			return nil, v2input.ErrInvalid
		}
	}
	policy, err := canonical.Marshal(map[string]any{"style_weights": styleModels, "operation_locks": operationModels})
	if err != nil || len(policy) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	native.policy = policy
	native.contract = "RenderComposition"
	return &ExportFrozenCompositionValidator{base: base, reader: reader, native: native}, nil
}

func (v *ExportFrozenCompositionValidator) ExportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if v == nil || v.base == nil || v.reader == nil || v.native == nil {
		return nil, v2input.ErrInvalid
	}
	// Own before any adapter can mutate its input buffer.
	body := append([]byte(nil), raw...)
	children, err := v.base.ExportDocumentChildren(ctx, entry, append([]byte(nil), body...))
	if err != nil {
		return nil, err
	}
	children = append([]v2input.Entry(nil), children...)
	if entry.Contract != "FrozenRenderRecipe" {
		return children, ctx.Err()
	}
	if !validImportRef(entry.Ref) || rawSHA(body) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(body)
	if err != nil {
		return nil, err
	}
	var authored struct {
		Base      v2input.Ref   `json:"base_style"`
		Overrides []v2input.Ref `json:"overrides"`
	}
	if err := json.Unmarshal(frozen.Recipe, &authored); err != nil {
		return nil, err
	}
	if len(authored.Overrides) > 128 {
		return nil, v2input.ErrInvalid
	}
	entries := []v2input.Entry{{Ref: frozen.Scalars.Source.Source, Contract: "WorkingImage"}, {Ref: authored.Base, Contract: "StyleV2"}}
	for _, ref := range authored.Overrides {
		entries = append(entries, v2input.Entry{Ref: ref, Contract: "Override"})
	}
	bundle := exportCompositionBundle{Schema: 1, Overrides: []exportCompositionDocument{}}
	remaining := int64(128 << 20)
	for index, dependency := range entries {
		present := false
		for _, child := range children {
			if child.Ref.ArtifactID == dependency.Ref.ArtifactID {
				if child != dependency {
					return nil, v2input.ErrInvalid
				}
				present = true
			}
		}
		if !present {
			return nil, v2input.ErrInvalid
		}
		node, err := v.reader.ResolveExportNode(ctx, dependency)
		if err != nil {
			return nil, err
		}
		document, err := ownExportCompositionDocument(ctx, node, dependency)
		if err != nil {
			return nil, err
		}
		remaining -= int64(len(document.Body))
		if remaining < 0 {
			return nil, v2input.ErrInvalid
		}
		switch index {
		case 0:
			bundle.Source = document
		case 1:
			bundle.Base = document
		default:
			bundle.Overrides = append(bundle.Overrides, document)
		}
	}
	encoded, err := canonical.Marshal(bundle)
	if err != nil || len(encoded) > 128<<20 {
		return nil, v2input.ErrInvalid
	}
	local := v2input.Entry{Ref: v2input.Ref{ArtifactID: entry.Ref.ArtifactID, SHA256: rawSHA(encoded)}, Contract: "RenderComposition"}
	if _, err := v.native.ExportDocumentChildren(ctx, local, encoded); err != nil {
		return nil, err
	}
	// Recheck catalogue/object authority after native IO, without retaining a
	// second complete set of document bodies or trusting an earlier catalogue read.
	for _, dependency := range entries {
		node, err := v.reader.ResolveExportNode(ctx, dependency)
		if err != nil {
			return nil, err
		}
		if _, err := ownExportCompositionDocument(ctx, node, dependency); err != nil {
			return nil, err
		}
	}
	return children, ctx.Err()
}

func ownExportCompositionDocument(ctx context.Context, node ExportClosureNode, expected v2input.Entry) (exportCompositionDocument, error) {
	var zero exportCompositionDocument
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if node.Entry != expected || !validImportRef(expected.Ref) || len(node.Body) < 2 || len(node.Body) > 8<<20 || rawSHA(node.Body) != expected.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	body := append([]byte(nil), node.Body...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) {
		return zero, v2input.ErrInvalid
	}
	return exportCompositionDocument{SHA: expected.Ref.SHA256, Body: body}, nil
}

var _ ExportTypedDocumentValidator = (*ExportFrozenCompositionValidator)(nil)
