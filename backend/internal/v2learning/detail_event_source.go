package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// PortraitDetailProducerCandidate comes only from the scoped SQL Apply-v4
// event-link source. It is not an object locator and cannot by itself
// authorize publication.
type PortraitDetailProducerCandidate struct {
	AssetVersionID uuid.UUID
	Source         v2input.Ref
	ConsentID      uuid.UUID
	ConsentEpoch   int64
	RunID          uuid.UUID
	TaskID         uuid.UUID
	ResultID       uuid.UUID
	AttemptID      uuid.UUID
	Image          v2input.Ref
	Params         v2input.Ref
	Resolved       v2input.Ref
}

func validPortraitDetailProducerCandidate(source PortraitDetailProducerCandidate,
	candidate TrainingCandidate, render TrainingRenderCase) bool {
	return source.AssetVersionID != uuid.Nil &&
		candidate.EventID != uuid.Nil && candidate.ConsentLive &&
		candidate.SourceReady && candidate.BaseReady && candidate.ApprovedReady &&
		source.AssetVersionID == candidate.AssetVersionID &&
		source.AssetVersionID == render.Binding.AssetVersionID &&
		source.ConsentID != uuid.Nil &&
		source.ConsentID == candidate.ConsentID &&
		source.ConsentEpoch > 0 && source.ConsentEpoch == candidate.ConsentEpoch &&
		source.Source == render.Binding.Source &&
		source.Source == render.WorkingImage &&
		source.Source.SHA256 == candidate.SourceContentSHA &&
		source.RunID != uuid.Nil && source.TaskID != uuid.Nil &&
		source.ResultID != uuid.Nil && source.AttemptID != uuid.Nil &&
		validManifestRef(source.Image) &&
		source.Image.ArtifactID != source.Source.ArtifactID &&
		validManifestRef(source.Params) &&
		validManifestRef(source.Resolved) &&
		source.Image.ArtifactID != source.Params.ArtifactID &&
		source.Image.ArtifactID != source.Resolved.ArtifactID &&
		source.Params.ArtifactID != source.Resolved.ArtifactID &&
		validManifestRef(render.Protected) &&
		render.Protected.ArtifactID != source.Source.ArtifactID &&
		render.Protected.ArtifactID != source.Image.ArtifactID &&
		render.Protected.ArtifactID != source.Params.ArtifactID &&
		render.Protected.ArtifactID != source.Resolved.ArtifactID
}

// LoadPortraitDetailProducerCandidate must use the same authenticated
// transaction as the training snapshot. SQL refuses missing/ambiguous Apply
// paths and verifies the event's immutable producer link. The caller must
// still read and verify physical documents and register the input closure.
func LoadPortraitDetailProducerCandidate(ctx context.Context, tx pgx.Tx,
	start TrainingStart, candidate TrainingCandidate,
	render TrainingRenderCase) (PortraitDetailProducerCandidate, error) {
	if tx == nil || ctx.Err() != nil ||
		start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		candidate.EventID == uuid.Nil || candidate.AssetVersionID == uuid.Nil {
		if err := ctx.Err(); err != nil {
			return PortraitDetailProducerCandidate{}, err
		}
		return PortraitDetailProducerCandidate{}, ErrInvalidTrainingDraft
	}
	var source PortraitDetailProducerCandidate
	err := tx.QueryRow(ctx, `SELECT asset_version_id,source_id,source_sha,
 consent_id,consent_epoch,run_id,task_id,result_id,attempt_id,
 image_id,image_sha,params_id,params_sha,resolved_id,resolved_sha
 FROM photo.v23_portrait_detail_event_link_source($1,$2,$3,$4,$5)`,
		start.TenantID, start.ProjectID, start.ActorID,
		start.AuthSessionID, candidate.EventID).Scan(
		&source.AssetVersionID, &source.Source.ArtifactID,
		&source.Source.SHA256, &source.ConsentID, &source.ConsentEpoch,
		&source.RunID, &source.TaskID, &source.ResultID,
		&source.AttemptID, &source.Image.ArtifactID, &source.Image.SHA256,
		&source.Params.ArtifactID, &source.Params.SHA256,
		&source.Resolved.ArtifactID, &source.Resolved.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return PortraitDetailProducerCandidate{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return PortraitDetailProducerCandidate{}, err
	}
	if err := ctx.Err(); err != nil {
		return PortraitDetailProducerCandidate{}, err
	}
	if !validPortraitDetailProducerCandidate(source, candidate, render) {
		return PortraitDetailProducerCandidate{}, ErrInvalidTrainingDraft
	}
	return source, nil
}
