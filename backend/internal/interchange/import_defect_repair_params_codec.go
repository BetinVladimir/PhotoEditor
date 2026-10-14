package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ImportDefectRepairParamsCodec preserves authored repair controls while
// rebuilding every dependency under the target asset-version authority.
type ImportDefectRepairParamsCodec struct {
	Qualifier ImportLayerOperationQualifier
	Masks     ImportDependencyCodec
	Fallback  ImportDependencyCodec
}

func isImportDefectRepairParams(kind string, raw []byte) bool {
	if kind != "Params" {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, proposal := fields["proposal"]
	_, review := fields["review"]
	_, resolved := fields["resolved"]
	_, protected := fields["protected"]
	_, regions := fields["regions"]
	return proposal && review && resolved && protected && !regions
}

func (c ImportDefectRepairParamsCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !isImportDefectRepairParams(kind, raw) {
		if c.Fallback != nil {
			return c.Fallback.RemapImportDocument(ctx, kind, raw, old, target, resolve)
		}
		return nil, &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	if c.Qualifier == nil || c.Masks == nil || resolve == nil || !validImportRef(old.Source) || !validImportRef(target.Source) || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil || old.AssetVersionID == target.AssetVersionID {
		return nil, v2input.ErrInvalid
	}
	if old.CanvasHash != target.CanvasHash {
		return nil, &ImportRecipeUnsupported{Reason: "params_canvas_remap_required"}
	}
	if err := validateImportDefectParams(raw); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var image v2input.Ref
	if err := json.Unmarshal(fields["image"], &image); err != nil || image != old.Source {
		return nil, v2input.ErrInvalid
	}
	var err error
	for _, dependency := range []struct{ field, kind string }{
		{"proposal", "DefectCandidates"},
		{"review", "DefectReview"},
		{"resolved", "ResolvedStyle"},
	} {
		var archived v2input.Ref
		if err := json.Unmarshal(fields[dependency.field], &archived); err != nil || !validImportRef(archived) {
			return nil, v2input.ErrInvalid
		}
		mapped, err := resolve(archived, dependency.kind)
		if err != nil {
			return nil, err
		}
		if !validImportRef(mapped) || mapped.ArtifactID == archived.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		fields[dependency.field], err = canonical.Marshal(mapped)
		if err != nil {
			return nil, err
		}
	}
	fields["image"], err = canonical.Marshal(target.Source)
	if err != nil {
		return nil, err
	}
	fields["protected"], err = c.Masks.RemapImportDocument(ctx, "MaskRef", fields["protected"], old, target, resolve)
	if err != nil {
		return nil, err
	}
	prepared, err := canonical.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func validateImportDefectParams(raw []byte) error {
	if len(raw) > 1<<20 {
		return v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return v2input.ErrInvalid
	}
	_, err = v2draft.DocumentChildren(raw, "Params", v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)})
	return err
}

func (c ImportDefectRepairParamsCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isImportDefectRepairParams(kind, raw) {
		if c.Fallback != nil {
			return c.Fallback.QualifyRemappedImportDocument(ctx, kind, raw, target)
		}
		return &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	if c.Qualifier == nil || c.Masks == nil || !validImportRef(target.Source) || target.AssetVersionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	if err := validateImportDefectParams(raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var image v2input.Ref
	if err := json.Unmarshal(fields["image"], &image); err != nil || image != target.Source {
		return v2input.ErrInvalid
	}
	if err := c.Masks.QualifyRemappedImportDocument(ctx, "MaskRef", fields["protected"], target); err != nil {
		return err
	}
	return c.Qualifier.QualifyImportedLayerOperation(ctx, "defects.repair", "1", append([]byte(nil), raw...), target)
}

func (c ImportDefectRepairParamsCodec) BindImportDocumentSource(source v2input.Ref) ImportDependencyCodec {
	if binder, ok := c.Fallback.(ImportDocumentSourceBinder); ok {
		c.Fallback = binder.BindImportDocumentSource(source)
	}
	return c
}
