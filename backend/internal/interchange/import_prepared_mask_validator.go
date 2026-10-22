package interchange

import (
	"context"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Witness insertion follows complete current model/source/physical mask checks.
// Geometry re-reads this published parent without recursively invoking semantics.
type ImportPreparedMaskDocumentValidator struct {
	Binding      v2input.SessionVersionBinding
	Qualifier    ImportMaskQualifier
	Witnesses    *ImportPreparedMaskWitnesses
	LUT          *ImportPreparedLUTWitnesses
	DefectLabels *ImportPreparedDefectLabelWitnesses
	Other        ImportTypedDocumentValidator
}

func (v ImportPreparedMaskDocumentValidator) ImportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entry.Contract != "MaskRef" {
		if v.Other == nil {
			return nil, v2input.ErrInvalid
		}
		children, err := v.Other.ImportDocumentChildren(ctx, entry, append([]byte(nil), raw...))
		if err != nil {
			return nil, err
		}
		if entry.Contract == "Override" || entry.Contract == "DefectProposeReport" || isImportStatsParams(entry.Contract, raw) || isImportConservativeParams(entry.Contract, raw) {
			if err := v.rememberInlineOverrideMasks(ctx, entry, raw); err != nil {
				return nil, err
			}
		}
		if entry.Contract == "ResolvedStyle" {
			if v.LUT == nil {
				return nil, v2input.ErrInvalid
			}
			if err := v.LUT.Remember(ctx, entry, raw, v.Binding, children); err != nil {
				return nil, err
			}
		}
		if entry.Contract == "DefectCandidates" {
			if v.DefectLabels == nil {
				return nil, v2input.ErrInvalid
			}
			if err := v.DefectLabels.Remember(ctx, entry, raw, v.Binding, children); err != nil {
				return nil, err
			}
		}
		return children, ctx.Err()
	}
	if v.Qualifier == nil || v.Witnesses == nil || !validImportRef(entry.Ref) || rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	mask, err := review.ParseMaskRef(body)
	if err != nil {
		return nil, err
	}
	if err := v.Qualifier.QualifyImportedMask(ctx, mask, v.Binding); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	children, err := (preparedMaskGeometryValidator{}).ImportDocumentChildren(ctx, entry, body)
	if err != nil {
		return nil, err
	}
	if err := v.Witnesses.remember(entry.Ref, body, v.Binding); err != nil {
		return nil, err
	}
	return children, ctx.Err()
}

type ImportPreparedResolverRouter struct {
	JSON, Working, Masks, Warps, LUT, DefectLabels, DefectPNGs, Assets ImportClosureNodeResolver
	HistoricalJSON, HistoricalArrays ImportClosureNodeResolver
 CurrentSource v2input.Ref
 HistoricalBinaryRefs map[v2input.Entry]bool
 Canonical                                              *ImportWorkingWitnesses
}

func (r ImportPreparedResolverRouter) ResolveImportNode(ctx context.Context, entry v2input.Entry) (ImportClosureNode, error) {
	if err := ctx.Err(); err != nil {
		return ImportClosureNode{}, err
	}
	var selected ImportClosureNodeResolver
	switch entry.Contract {
	case "WorkingImage":
        selected=r.Working;if validImportRef(r.CurrentSource)&&entry.Ref!=r.CurrentSource{selected=r.HistoricalJSON}
 case "WorkingPixels", "WorkingAlpha":
        selected=r.Working;if r.HistoricalBinaryRefs[entry]{selected=r.HistoricalArrays}
	case "MaskPixels":
		selected = r.Masks
	case "DenseWarpField":
		if _, canonical := r.Canonical.lookup(entry); canonical {
			selected = r.Working
		} else if r.HistoricalBinaryRefs[entry] {selected=r.HistoricalArrays
        } else {
			selected = r.Warps
		}
	case "StyleBackgroundAsset":
		selected = r.Assets
	case "AbsoluteLUTArray":
		selected = r.LUT
	case "DefectLabelPNG":
		selected = r.DefectPNGs
	case "DefectLabelMap":
		selected = r.DefectLabels
	case "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates", "DefectProposeReport", "EditingRecipeV3", "FaceLandmarks", "FaceBoxes":
		selected = r.JSON
	default:
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	if selected == nil {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	return selected.ResolveImportNode(ctx, entry)
}
