package v2learning

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/review"
)

// LoadReviewedDustForCandidate obtains the sole current sensor-dust Apply
// review from SQL, never from a caller-selected review ID. The physical
// artifact bytes are read and checked later by PrepareReviewedDustHoldout.
func LoadReviewedDustForCandidate(ctx context.Context, tx pgx.Tx,
	start TrainingStart, candidate TrainingCandidate,
	render TrainingRenderCase) (ReviewedDustSource, error) {
	if tx == nil || start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		candidate.EventID == uuid.Nil || candidate.AssetVersionID == uuid.Nil {
		return ReviewedDustSource{}, ErrInvalidTrainingDraft
	}
	var source ReviewedDustSource
	var actorID, consentID uuid.UUID
	var revision, consentEpoch int64
	var decisionHash string
	var selectionRaw []byte
	err := tx.QueryRow(ctx, `SELECT review_id,review_run_id,actor_id,
 decision_hash,revision,selection,asset_version_id,consent_id,consent_epoch,
 image_id,image_sha,stage_image_id,stage_image_sha,
 proposal_id,proposal_sha,params_id,params_sha,detect_task_id,repair_task_id
 FROM photo.v23_reviewed_dust_event_source($1,$2,$3,$4,$5)`,
		start.TenantID, start.ProjectID, start.ActorID,
		start.AuthSessionID, candidate.EventID).Scan(
		&source.Decision.ReviewID, &source.Decision.RunID,
		&actorID, &decisionHash, &revision, &selectionRaw,
		&source.AssetVersionID, &consentID, &consentEpoch,
		&source.Image.ArtifactID, &source.Image.SHA256,
		&source.StageImage.ArtifactID, &source.StageImage.SHA256,
		&source.Proposal.ArtifactID, &source.Proposal.SHA256,
		&source.Params.ArtifactID, &source.Params.SHA256,
		&source.DetectTaskID, &source.RepairTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewedDustSource{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return ReviewedDustSource{}, err
	}
	if source.AssetVersionID != candidate.AssetVersionID ||
		consentID != candidate.ConsentID || consentEpoch != candidate.ConsentEpoch ||
		source.Image.SHA256 != candidate.SourceContentSHA ||
		source.Image != render.WorkingImage ||
		source.Image != render.Binding.Source ||
		source.AssetVersionID != render.Binding.AssetVersionID ||
		source.DetectTaskID == uuid.Nil || source.RepairTaskID == uuid.Nil ||
		source.DetectTaskID == source.RepairTaskID ||
		!validManifestRef(source.StageImage) ||
		source.StageImage.ArtifactID == source.Image.ArtifactID ||
		!validManifestRef(source.Proposal) || !validManifestRef(source.Params) {
		return ReviewedDustSource{}, ErrInvalidTrainingDraft
	}
	var selection review.DustSelection
	if json.Unmarshal(selectionRaw, &selection) != nil {
		return ReviewedDustSource{}, ErrInvalidTrainingDraft
	}
	source.Decision.TenantID = start.TenantID
	source.Decision.ActorID = actorID
	source.Decision.Decision = "accept"
	source.Decision.Route = "repair"
	source.Decision.DecisionHash = decisionHash
	source.Decision.Revision = revision
	source.Decision.Selection = selection
	if _, _, _, err := CompileFrozenDustHoldoutEvidence(source.Decision,
		candidate.EventID, candidate.AssetVersionID, source.Image,
		source.StageImage, render.Protected, source.Proposal,
		source.Params); err != nil {
		return ReviewedDustSource{}, ErrInvalidTrainingDraft
	}
	return source, nil
}
