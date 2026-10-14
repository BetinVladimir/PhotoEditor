package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ImportResolvedQualifier must run the complete ResolvedStyle schema and
// installed model/runtime checks. A hash or JSON parse is not a qualifier.
type ImportResolvedQualifier interface {
	ValidateArchivedImportResolved(context.Context, []byte, v2input.SessionVersionBinding) error
	QualifyImportedResolved(context.Context, []byte, v2input.SessionVersionBinding) error
}

type ImportResolvedCodec struct {
	Qualifier ImportResolvedQualifier
	Override  ImportDependencyCodec
}

func (c ImportResolvedCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind != "ResolvedStyle" {
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Qualifier == nil || resolve == nil || !validImportRef(old.Source) || !validImportRef(target.Source) {
		return nil, v2input.ErrInvalid
	}
	if old.CanvasHash != target.CanvasHash {
		return nil, &ImportRecipeUnsupported{Reason: "resolved_canvas_remap_required"}
	}
	if len(raw) < 2 || len(raw) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	if err := c.Qualifier.ValidateArchivedImportResolved(ctx, append([]byte(nil), raw...), old); err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if _, present := body["hash"]; !present {
		return nil, v2input.ErrInvalid
	}
	delete(body, "hash")
	var params map[string]json.RawMessage
	if err := json.Unmarshal(body["params"], &params); err != nil || params == nil {
		return nil, v2input.ErrInvalid
	}
	var image v2input.Ref
	if err := json.Unmarshal(params["image"], &image); err != nil || image != old.Source {
		return nil, v2input.ErrInvalid
	}
	params["image"], err = canonical.Marshal(target.Source)
	if err != nil {
		return nil, err
	}
	for _, child := range []struct{ field, contract string }{{"style", "StyleV2"}, {"stats", "Stats20"}} {
		if value, present := params[child.field]; present && string(value) != "null" {
			mapped, err := remapResolvedRef(value, child.contract, resolve)
			if err != nil {
				return nil, err
			}
			params[child.field] = mapped
		}
	}
	if value, present := params["override"]; present && string(value) != "null" {
		if c.Override == nil {
			return nil, &ImportRecipeUnsupported{Reason: "resolved_override_codec_not_installed"}
		}
		prepared, err := c.Override.RemapImportDocument(ctx, "Override", value, old, target, resolve)
		if err != nil {
			return nil, err
		}
		params["override"] = prepared
	}
	body["params"], err = canonical.Marshal(params)
	if err != nil {
		return nil, err
	}
	if value, present := body["absolute_lut"]; present && string(value) != "null" {
		var lut map[string]json.RawMessage
		if err := json.Unmarshal(value, &lut); err != nil || lut == nil {
			return nil, v2input.ErrInvalid
		}
		lut["artifact"], err = remapResolvedRef(lut["artifact"], "AbsoluteLUTArray", resolve)
		if err != nil {
			return nil, err
		}
		body["absolute_lut"], err = canonical.Marshal(lut)
		if err != nil {
			return nil, err
		}
	}
	hash, err := canonical.Hash(body)
	if err != nil {
		return nil, err
	}
	body["hash"], err = canonical.Marshal(hash)
	if err != nil {
		return nil, err
	}
	prepared, err := canonical.Marshal(body)
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func remapResolvedRef(raw []byte, contract string, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	var ref v2input.Ref
	if err := json.Unmarshal(raw, &ref); err != nil || !validImportRef(ref) {
		return nil, v2input.ErrInvalid
	}
	mapped, err := resolve(ref, contract)
	if err != nil {
		return nil, err
	}
	if !validImportRef(mapped) || mapped.ArtifactID == ref.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	return canonical.Marshal(mapped)
}

func (c ImportResolvedCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind != "ResolvedStyle" {
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Qualifier == nil || !validImportRef(target.Source) {
		return v2input.ErrInvalid
	}
	if err := c.Qualifier.QualifyImportedResolved(ctx, append([]byte(nil), raw...), target); err != nil {
		return err
	}
	return ctx.Err()
}
