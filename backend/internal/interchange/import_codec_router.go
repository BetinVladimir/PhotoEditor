package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// Closed typed routing prevents a fallback intended for one schema from
// accidentally accepting a different dependency contract.
type ImportRecipeCodecRouter struct {
	codecs map[string]ImportDependencyCodec
}

func NewImportRecipeCodecRouter(style, correction, override, layers, masks, params ImportDependencyCodec) (*ImportRecipeCodecRouter, error) {
	codecs := map[string]ImportDependencyCodec{"StyleV2": style, "CorrectionPlan": correction, "Override": override, "LayerStack": layers, "MaskRef": masks, "Params": params}
	for _, codec := range codecs {
		if codec == nil {
			return nil, v2input.ErrInvalid
		}
	}
	return &ImportRecipeCodecRouter{codecs: codecs}, nil
}

// WithResolved returns a separate router so an incomplete deployment cannot
// silently enable ResolvedStyle for existing scheduler instances.
func (r *ImportRecipeCodecRouter) WithResolved(codec ImportDependencyCodec) (*ImportRecipeCodecRouter, error) {
	if r == nil || codec == nil || len(r.codecs) != 6 || r.codecs["ResolvedStyle"] != nil {
		return nil, v2input.ErrInvalid
	}
	owned := make(map[string]ImportDependencyCodec, len(r.codecs)+1)
	for kind, existing := range r.codecs {
		if existing == nil {
			return nil, v2input.ErrInvalid
		}
		owned[kind] = existing
	}
	owned["ResolvedStyle"] = codec
	return &ImportRecipeCodecRouter{codecs: owned}, nil
}
func (r *ImportRecipeCodecRouter) WithStats(codec ImportDependencyCodec) (*ImportRecipeCodecRouter, error) {
	if r == nil || codec == nil || r.codecs["Stats20"] != nil {
		return nil, v2input.ErrInvalid
	}
	owned := make(map[string]ImportDependencyCodec, len(r.codecs)+1)
	for kind, existing := range r.codecs {
		if existing == nil {
			return nil, v2input.ErrInvalid
		}
		owned[kind] = existing
	}
	owned["Stats20"] = codec
	return &ImportRecipeCodecRouter{codecs: owned}, nil
}
func (r *ImportRecipeCodecRouter) WithDefectReview(codec ImportDependencyCodec) (*ImportRecipeCodecRouter, error) {
	if r == nil || codec == nil || r.codecs["DefectReview"] != nil {
		return nil, v2input.ErrInvalid
	}
	owned := make(map[string]ImportDependencyCodec, len(r.codecs)+1)
	for kind, existing := range r.codecs {
		if existing == nil {
			return nil, v2input.ErrInvalid
		}
		owned[kind] = existing
	}
	owned["DefectReview"] = codec
	return &ImportRecipeCodecRouter{codecs: owned}, nil
}
func (r *ImportRecipeCodecRouter) WithDefectCandidates(codec ImportDependencyCodec) (*ImportRecipeCodecRouter, error) {
	if r == nil || codec == nil || r.codecs["DefectCandidates"] != nil {
		return nil, v2input.ErrInvalid
	}
	owned := make(map[string]ImportDependencyCodec, len(r.codecs)+1)
	for kind, existing := range r.codecs {
		if existing == nil {
			return nil, v2input.ErrInvalid
		}
		owned[kind] = existing
	}
	owned["DefectCandidates"] = codec
	return &ImportRecipeCodecRouter{codecs: owned}, nil
}
func (r *ImportRecipeCodecRouter) WithDustCandidates(codec ImportDependencyCodec) (*ImportRecipeCodecRouter, error) {
	if r == nil || codec == nil || r.codecs["DustCandidates"] != nil {
		return nil, v2input.ErrInvalid
	}
	owned := make(map[string]ImportDependencyCodec, len(r.codecs)+1)
	for kind, existing := range r.codecs {
		if existing == nil {
			return nil, v2input.ErrInvalid
		}
		owned[kind] = existing
	}
	owned["DustCandidates"] = codec
	return &ImportRecipeCodecRouter{codecs: owned}, nil
}
func (r *ImportRecipeCodecRouter) BindImportDocumentSource(source v2input.Ref) ImportDependencyCodec {
	if r == nil || !validImportRef(source) {
		return nil
	}
	owned := map[string]ImportDependencyCodec{}
	for kind, codec := range r.codecs {
		if binder, ok := codec.(ImportDocumentSourceBinder); ok {
			codec = binder.BindImportDocumentSource(source)
		}
		if codec == nil {
			return nil
		}
		owned[kind] = codec
	}
	return &ImportRecipeCodecRouter{codecs: owned}
}
func (r *ImportRecipeCodecRouter) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || resolve == nil {
		return nil, v2input.ErrInvalid
	}
	codec := r.codecs[kind]
	if codec == nil {
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	result, err := codec.RemapImportDocument(ctx, kind, append([]byte(nil), raw...), old, target, resolve)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte(nil), result...), nil
}
func (r *ImportRecipeCodecRouter) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return v2input.ErrInvalid
	}
	codec := r.codecs[kind]
	if codec == nil {
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if err := codec.QualifyRemappedImportDocument(ctx, kind, append([]byte(nil), raw...), target); err != nil {
		return err
	}
	return ctx.Err()
}
