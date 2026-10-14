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

// Fresh confirmation is independent of the archived boolean. The installed
// authority must validate current user/project approval for the new version.
type ImportPortraitRegionConfirmation interface {
	QualifyImportedPortraitRegion(context.Context, []byte, []byte, v2input.SessionVersionBinding, v2input.SessionVersionBinding) error
	QualifyPreparedPortraitRegion(context.Context, []byte, v2input.SessionVersionBinding) error
}

type ImportPortraitDetailParamsCodec struct {
	Qualifier ImportLayerOperationQualifier
	Masks     ImportDependencyCodec
	Confirmed ImportPortraitRegionConfirmation
	Fallback  ImportDependencyCodec
}

func isImportPortraitDetailParams(kind string, raw []byte) bool {
	if kind != "Params" {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, image := fields["image"]
	_, resolved := fields["resolved"]
	_, regions := fields["regions"]
	_, legacy := fields["base_aligned"]
	return image && resolved && regions && !legacy
}

func (c ImportPortraitDetailParamsCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !isImportPortraitDetailParams(kind, raw) {
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
	raw = append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	if _, err := v2draft.DocumentChildren(raw, "Params", v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var image, resolved v2input.Ref
	if err := json.Unmarshal(fields["image"], &image); err != nil || image != old.Source {
		return nil, v2input.ErrInvalid
	}
	if err := json.Unmarshal(fields["resolved"], &resolved); err != nil || !validImportRef(resolved) {
		return nil, v2input.ErrInvalid
	}
	mapped, err := resolve(resolved, "ResolvedStyle")
	if err != nil {
		return nil, err
	}
	if !validImportRef(mapped) || mapped.ArtifactID == resolved.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	fields["image"], err = canonical.Marshal(target.Source)
	if err != nil {
		return nil, err
	}
	fields["resolved"], err = canonical.Marshal(mapped)
	if err != nil {
		return nil, err
	}
	var regions []map[string]json.RawMessage
	if err := json.Unmarshal(fields["regions"], &regions); err != nil || regions == nil || len(regions) > 128 {
		return nil, v2input.ErrInvalid
	}
	for _, region := range regions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		archived, err := canonical.Marshal(region)
		if err != nil {
			return nil, err
		}
		var confirmed bool
		if err := json.Unmarshal(region["confirmed"], &confirmed); err != nil {
			return nil, v2input.ErrInvalid
		}
		if confirmed && c.Confirmed == nil {
			return nil, &ImportRecipeUnsupported{Reason: "portrait_region_requires_new_confirmation"}
		}
		prepared, err := c.Masks.RemapImportDocument(ctx, "MaskRef", region["mask"], old, target, resolve)
		if err != nil {
			return nil, err
		}
		region["mask"] = prepared
		if confirmed {
			current, err := canonical.Marshal(region)
			if err != nil {
				return nil, err
			}
			if err := c.Confirmed.QualifyImportedPortraitRegion(ctx, archived, current, old, target); err != nil {
				return nil, err
			}
		}
	}
	fields["regions"], err = canonical.Marshal(regions)
	if err != nil {
		return nil, err
	}
	if value, present := fields["protected"]; present {
		var masks []json.RawMessage
		if err := json.Unmarshal(value, &masks); err != nil || masks == nil || len(masks) > 32 {
			return nil, v2input.ErrInvalid
		}
		for index, mask := range masks {
			masks[index], err = c.Masks.RemapImportDocument(ctx, "MaskRef", mask, old, target, resolve)
			if err != nil {
				return nil, err
			}
		}
		fields["protected"], err = canonical.Marshal(masks)
		if err != nil {
			return nil, err
		}
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

func (c ImportPortraitDetailParamsCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isImportPortraitDetailParams(kind, raw) {
		if c.Fallback != nil {
			return c.Fallback.QualifyRemappedImportDocument(ctx, kind, raw, target)
		}
		return &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	if c.Qualifier == nil || c.Masks == nil || !validImportRef(target.Source) || target.AssetVersionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	if _, err := v2draft.DocumentChildren(raw, "Params", v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}); err != nil {
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
	var regions []map[string]json.RawMessage
	if err := json.Unmarshal(fields["regions"], &regions); err != nil {
		return err
	}
	for _, region := range regions {
		if err := c.Masks.QualifyRemappedImportDocument(ctx, "MaskRef", region["mask"], target); err != nil {
			return err
		}
		var confirmed bool
		if err := json.Unmarshal(region["confirmed"], &confirmed); err != nil {
			return v2input.ErrInvalid
		}
		if confirmed {
			if c.Confirmed == nil {
				return &ImportRecipeUnsupported{Reason: "portrait_region_requires_new_confirmation"}
			}
			body, err := canonical.Marshal(region)
			if err != nil {
				return err
			}
			if err := c.Confirmed.QualifyPreparedPortraitRegion(ctx, body, target); err != nil {
				return err
			}
		}
	}
	if value, present := fields["protected"]; present {
		var masks []json.RawMessage
		if err := json.Unmarshal(value, &masks); err != nil {
			return err
		}
		for _, mask := range masks {
			if err := c.Masks.QualifyRemappedImportDocument(ctx, "MaskRef", mask, target); err != nil {
				return err
			}
		}
	}
	if err := c.Qualifier.QualifyImportedLayerOperation(ctx, "portrait.detail", "1", append([]byte(nil), raw...), target); err != nil {
		return err
	}
	for _, region := range regions {
		var confirmed bool
		if err := json.Unmarshal(region["confirmed"], &confirmed); err != nil {
			return v2input.ErrInvalid
		}
		if confirmed {
			body, err := canonical.Marshal(region)
			if err != nil {
				return err
			}
			if err := c.Confirmed.QualifyPreparedPortraitRegion(ctx, body, target); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
func (c ImportPortraitDetailParamsCodec) BindImportDocumentSource(source v2input.Ref) ImportDependencyCodec {
	if binder, ok := c.Fallback.(ImportDocumentSourceBinder); ok {
		c.Fallback = binder.BindImportDocumentSource(source)
	}
	return c
}
