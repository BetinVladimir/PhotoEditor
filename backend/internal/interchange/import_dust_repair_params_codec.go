package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// A reviewed dust repair needs a newly authorized decision for the remapped
// proposal. The archived selection only supplies context to the issuer.
type ImportDustReviewAuthority interface {
	ReissueImportedDustReview(context.Context, review.DustSelection, review.DustSelection, v2input.SessionVersionBinding, v2input.SessionVersionBinding) (review.DustSelection, error)
	QualifyPreparedDustReview(context.Context, review.DustSelection, v2input.SessionVersionBinding) error
}

type importDustRepairParams struct {
	Image    v2input.Ref           `json:"image"`
	Proposal *v2input.Ref          `json:"proposal"`
	Review   *review.DustSelection `json:"review"`
}

func isImportDustRepairParams(kind string, raw []byte) bool {
	if kind != "Params" {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	_, image := fields["image"]
	_, proposal := fields["proposal"]
	_, decision := fields["review"]
	return image && proposal && decision
}

func decodeImportDustRepairParams(raw []byte) (importDustRepairParams, error) {
	var p importDustRepairParams
	if len(raw) < 2 || len(raw) > 1<<20 {
		return p, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) || decodeImportDocument(raw, &p) != nil || !validImportRef(p.Image) || (p.Proposal == nil) != (p.Review == nil) {
		return p, v2input.ErrInvalid
	}
	if p.Proposal != nil {
		if !validImportRef(*p.Proposal) || p.Review.Proposal.ID != p.Proposal.ArtifactID || p.Review.Proposal.SHA256 != p.Proposal.SHA256 || p.Review.Base.ID != p.Image.ArtifactID || p.Review.Base.SHA256 != p.Image.SHA256 || review.ValidateDustSelectionStructure(*p.Review) != nil {
			return p, v2input.ErrInvalid
		}
	}
	return p, nil
}

type ImportDustRepairParamsCodec struct {
	Qualifier ImportLayerOperationQualifier
	Authority ImportDustReviewAuthority
	Fallback  ImportDependencyCodec
}

func (c ImportDustRepairParamsCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !isImportDustRepairParams(kind, raw) {
		if c.Fallback != nil {
			return c.Fallback.RemapImportDocument(ctx, kind, raw, old, target, resolve)
		}
		return nil, &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	p, err := decodeImportDustRepairParams(raw)
	if err != nil {
		return nil, err
	}
	if c.Qualifier == nil || resolve == nil || p.Image != old.Source || !validImportRef(target.Source) || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil || old.AssetVersionID == target.AssetVersionID || old.CanvasHash != target.CanvasHash {
		return nil, v2input.ErrInvalid
	}
	archived := p.Review
	if p.Proposal != nil {
		if c.Authority == nil {
			return nil, &ImportRecipeUnsupported{Reason: "dust_review_requires_new_authority"}
		}
		mapped, err := resolve(*p.Proposal, "DustCandidates")
		if err != nil {
			return nil, err
		}
		if !validImportRef(mapped) || mapped.ArtifactID == p.Proposal.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		p.Proposal = &mapped
		provisional := *archived
		provisional.Proposal = review.ArtifactRef{ID: mapped.ArtifactID, SHA256: mapped.SHA256}
		provisional.Base = review.ArtifactRef{ID: target.Source.ArtifactID, SHA256: target.Source.SHA256}
		reissued, err := c.Authority.ReissueImportedDustReview(ctx, *archived, provisional, old, target)
		if err != nil {
			return nil, err
		}
		if reissued.Version != provisional.Version || reissued.Proposal != provisional.Proposal || reissued.Base != provisional.Base || review.ValidateDustSelectionStructure(reissued) != nil {
			return nil, v2input.ErrInvalid
		}
		p.Review = &reissued
	}
	p.Image = target.Source
	prepared, err := canonical.Marshal(p)
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func (c ImportDustRepairParamsCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !isImportDustRepairParams(kind, raw) {
		if c.Fallback != nil {
			return c.Fallback.QualifyRemappedImportDocument(ctx, kind, raw, target)
		}
		return &ImportRecipeUnsupported{Reason: "params_contract_not_installed"}
	}
	p, err := decodeImportDustRepairParams(raw)
	if err != nil {
		return err
	}
	if c.Qualifier == nil || p.Image != target.Source || target.AssetVersionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	if p.Review != nil {
		if c.Authority == nil {
			return &ImportRecipeUnsupported{Reason: "dust_review_requires_new_authority"}
		}
		if err := c.Authority.QualifyPreparedDustReview(ctx, *p.Review, target); err != nil {
			return err
		}
	}
	return c.Qualifier.QualifyImportedLayerOperation(ctx, "sensor.dust_repair", "1", append([]byte(nil), raw...), target)
}

func (c ImportDustRepairParamsCodec) BindImportDocumentSource(source v2input.Ref) ImportDependencyCodec {
	if binder, ok := c.Fallback.(ImportDocumentSourceBinder); ok {
		c.Fallback = binder.BindImportDocumentSource(source)
	}
	return c
}
