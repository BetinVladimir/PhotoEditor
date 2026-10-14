package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type ReviewedDefectEventBinding struct {
	ReviewID       uuid.UUID
	AssetVersionID uuid.UUID
	Source         v2input.Ref
	ConsentID      uuid.UUID
	ConsentEpoch   int64
}

// LoadReviewedDefectForCandidate lets SQL choose the sole current review for
// an event. It never accepts a review ID from the caller. The later publisher
// must also prove that the approved recipe requests this exact repair effect.
func LoadReviewedDefectForCandidate(ctx context.Context, tx pgx.Tx,
	start TrainingStart, candidate TrainingCandidate,
	render TrainingRenderCase) (ReviewedDefectSource, error) {
	if tx == nil || start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		candidate.EventID == uuid.Nil {
		return ReviewedDefectSource{}, ErrInvalidTrainingDraft
	}
	var binding ReviewedDefectEventBinding
	err := tx.QueryRow(ctx, `SELECT review_id,asset_version_id,source_id,
 source_sha,consent_id,consent_epoch FROM
 photo.v23_reviewed_defect_event_source($1,$2,$3,$4,$5)`,
		start.TenantID, start.ProjectID, start.ActorID,
		start.AuthSessionID, candidate.EventID).Scan(
		&binding.ReviewID, &binding.AssetVersionID,
		&binding.Source.ArtifactID, &binding.Source.SHA256,
		&binding.ConsentID, &binding.ConsentEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewedDefectSource{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return ReviewedDefectSource{}, err
	}
	if !validReviewedDefectEventBinding(binding, candidate, render) {
		return ReviewedDefectSource{}, ErrInvalidTrainingDraft
	}
	source, err := LoadReviewedDefectSource(ctx, tx, start.TenantID,
		start.ProjectID, start.ActorID, start.AuthSessionID, binding.ReviewID)
	if err != nil {
		return ReviewedDefectSource{}, err
	}
	if source.Image != binding.Source || source.Decision.ReviewID != binding.ReviewID {
		return ReviewedDefectSource{}, ErrInvalidTrainingDraft
	}
	return source, nil
}

func validReviewedDefectEventBinding(binding ReviewedDefectEventBinding,
	candidate TrainingCandidate, render TrainingRenderCase) bool {
	return binding.ReviewID != uuid.Nil &&
		binding.AssetVersionID != uuid.Nil && binding.ConsentID != uuid.Nil &&
		binding.ConsentEpoch >= 1 && validManifestRef(binding.Source) &&
		binding.AssetVersionID == candidate.AssetVersionID &&
		binding.ConsentID == candidate.ConsentID &&
		binding.ConsentEpoch == candidate.ConsentEpoch &&
		binding.Source.SHA256 == candidate.SourceContentSHA &&
		binding.Source == render.WorkingImage &&
		binding.Source == render.Binding.Source &&
		binding.AssetVersionID == render.Binding.AssetVersionID
}
