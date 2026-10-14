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

// The current tenant must independently authorize the prepared decision.
// Archived accepted/rejected IDs and revision are evidence, not consent.
type ImportDefectReviewAuthority interface {
	ReissueImportedDefectReview(context.Context, review.DefectSelection, review.DefectSelection, v2input.SessionVersionBinding, v2input.SessionVersionBinding) (review.DefectSelection, error)
	QualifyPreparedDefectReview(context.Context, review.DefectSelection, v2input.SessionVersionBinding) error
}

type ImportDefectReviewCodec struct{ Authority ImportDefectReviewAuthority }

func (c ImportDefectReviewCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind != "DefectReview" {
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if resolve == nil || !validImportRef(old.Source) || !validImportRef(target.Source) || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil || old.AssetVersionID == target.AssetVersionID || old.CanvasHash != target.CanvasHash {
		return nil, v2input.ErrInvalid
	}
	if c.Authority == nil {
		return nil, &ImportRecipeUnsupported{Reason: "defect_review_requires_new_authority"}
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	archived, err := review.ParseDefectSelection(raw, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)})
	if err != nil || archived.Image.ID != old.Source.ArtifactID || archived.Image.SHA256 != old.Source.SHA256 {
		return nil, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, dependency := range []struct{ field, kind string }{{"proposal", "DefectCandidates"}, {"label_map", "DefectLabelMap"}} {
		var source v2input.Ref
		if err := json.Unmarshal(fields[dependency.field], &source); err != nil || !validImportRef(source) {
			return nil, v2input.ErrInvalid
		}
		mapped, err := resolve(source, dependency.kind)
		if err != nil {
			return nil, err
		}
		if !validImportRef(mapped) || mapped.ArtifactID == source.ArtifactID {
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
	provisional, err := canonical.Marshal(fields)
	if err != nil {
		return nil, err
	}
	current, err := review.ParseDefectSelection(provisional, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(provisional)})
	if err != nil {
		return nil, err
	}
	reissued, err := c.Authority.ReissueImportedDefectReview(ctx, archived, current, old, target)
	if err != nil {
		return nil, err
	}
	// Authority may replace candidate IDs and revision after checking the
	// target proposal. It may not redirect the decision to other artifacts.
	if reissued.Version != current.Version || reissued.Image != current.Image || reissued.Proposal != current.Proposal || reissued.LabelMap != current.LabelMap || review.ValidateDefectSelectionStructure(reissued) != nil {
		return nil, v2input.ErrInvalid
	}
	prepared, err := canonical.Marshal(reissued)
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func (c ImportDefectReviewCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind != "DefectReview" {
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Authority == nil {
		return &ImportRecipeUnsupported{Reason: "defect_review_requires_new_authority"}
	}
	selection, err := review.ParseDefectSelection(raw, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)})
	if err != nil || selection.Image.ID != target.Source.ArtifactID || selection.Image.SHA256 != target.Source.SHA256 {
		return v2input.ErrInvalid
	}
	return c.Authority.QualifyPreparedDefectReview(ctx, selection, target)
}
