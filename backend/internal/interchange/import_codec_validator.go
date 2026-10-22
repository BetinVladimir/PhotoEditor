package interchange

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Connects remapping semantics to the later published-document traversal.
// Children supplies schema-specific roles; complete physical reference equality
// prevents it from omitting a dependency or introducing an unrelated one.
type ImportCodecDocumentValidator struct {
	Binding  v2input.SessionVersionBinding
	Codec    ImportDependencyCodec
	Children ImportTypedDocumentValidator
}

func (v ImportCodecDocumentValidator) ImportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if v.Codec == nil || v.Children == nil || v.Binding.AssetVersionID == uuid.Nil || !validImportRef(v.Binding.Source) || !validImportRefForSHA(v.Binding.CanvasHash) || !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	switch entry.Contract {
	case "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates", "DefectProposeReport", "WorkingImage", "FaceLandmarks", "FaceBoxes":
	default:
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	body := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(body, normalized) || rawSHA(body) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	codec := v.Codec
	if binder, ok := codec.(ImportDocumentSourceBinder); ok {
		codec = binder.BindImportDocumentSource(entry.Ref)
	}
	if codec == nil {
		return nil, v2input.ErrInvalid
	}
	if err := codec.QualifyRemappedImportDocument(ctx, entry.Contract, append([]byte(nil), body...), v.Binding); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	children, err := v.Children.ImportDocumentChildren(ctx, entry, append([]byte(nil), body...))
	if err != nil {
		return nil, err
	}
	children = append([]v2input.Entry(nil), children...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	refs, err := importClosureDocumentRefs(body, entry.Contract, children)
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]v2input.Entry{}
	for _, child := range children {
		if !validImportRef(child.Ref) || child.Contract == "" || child.Ref.ArtifactID == entry.Ref.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		if _, exists := seen[child.Ref.ArtifactID]; exists {
			return nil, v2input.ErrInvalid
		}
		if refs[child.Ref.ArtifactID] != child.Ref {
			return nil, v2input.ErrInvalid
		}
		seen[child.Ref.ArtifactID] = child
	}
	if len(seen) != len(refs) {
		return nil, v2input.ErrInvalid
	}
	return children, ctx.Err()
}
