package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// PublishReviewedDustHoldout records exact prepared bytes after the recovery
// manifest commits. SQL rechecks the event-selected review during both phases.
func PublishReviewedDustHoldout(ctx context.Context,
	publisher artifactjson.Publisher, draft TrainingDraftPrepared,
	prepared PreparedReviewedDustHoldout) (v2input.Ref, error) {
	if publisher.Repository == nil || publisher.Objects == nil ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil ||
		draft.ActorID == uuid.Nil || draft.AuthSessionID == uuid.Nil ||
		prepared.Document.TenantID != draft.TenantID ||
		prepared.Document.EventID == uuid.Nil ||
		prepared.Document.ReviewID == uuid.Nil {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	ref, err := ReviewedDustHoldoutRef(draft.RunID,
		prepared.Document.EventID, prepared.SHA256)
	if err != nil {
		return v2input.Ref{}, err
	}
	expected := DustHoldoutExpected{TenantID: draft.TenantID,
		EventID:        prepared.Document.EventID,
		AssetVersionID: prepared.Document.AssetVersionID,
		ReviewID:       prepared.Document.ReviewID,
		RunID:          prepared.Document.RunID,
		ActorID:        prepared.Document.ActorID,
		DecisionHash:   prepared.Document.DecisionHash,
		Image:          prepared.Document.Image,
		StageImage:     prepared.Document.StageImage,
		Protection:     prepared.Document.Protection,
		Proposal:       prepared.Document.Proposal,
		Params:         prepared.Document.Params}
	if _, err := DecodeFrozenDustHoldoutEvidence(prepared.Body, ref, expected); err != nil {
		return v2input.Ref{}, err
	}
	matches := 0
	for _, item := range draft.Manifest.Examples {
		if item.EventID != prepared.Document.EventID {
			continue
		}
		if item.RenderCase.DustEvidence == nil ||
			*item.RenderCase.DustEvidence != ref ||
			item.RenderCase.DefectEvidence != nil ||
			item.RenderCase.Binding.AssetVersionID != prepared.Document.AssetVersionID ||
			item.RenderCase.WorkingImage != prepared.Document.Image ||
			item.RenderCase.Protected != prepared.Document.Protection {
			return v2input.Ref{}, ErrInvalidTrainingDraft
		}
		matches++
	}
	if matches != 1 {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	got, err := publisher.Publish(ctx, artifactjson.Scope{
		Tenant: draft.TenantID, Run: draft.RunID,
		Owner: prepared.Document.EventID, Purpose: "v23_dust_holdout",
		Review: prepared.Document.ReviewID, Actor: draft.ActorID,
		AuthSession:  draft.AuthSessionID,
		DecisionHash: prepared.Document.DecisionHash}, prepared.Body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if got.ID != ref.ArtifactID || got.SHA256 != ref.SHA256 {
		return v2input.Ref{}, ErrTrainingDraftConflict
	}
	return ref, nil
}
