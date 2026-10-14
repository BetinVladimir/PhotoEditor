package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// ReviewedDefectHoldoutRef is a recovery-stable training-run document ID.
// Its SQL counterpart in 01968 uses the same UUIDv5 name.
func ReviewedDefectHoldoutRef(runID, eventID uuid.UUID, sha string) (v2input.Ref, error) {
	if runID == uuid.Nil || eventID == uuid.Nil || !validSHA(sha) {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return v2input.Ref{ArtifactID: uuid.NewSHA1(runID,
		[]byte("v23-defect/"+eventID.String())), SHA256: sha}, nil
}

// PublishReviewedDefectHoldout records exact prepared bytes after the recovery
// manifest commits. The SQL repository rechecks live event/review authority
// during both Reserve and Complete; this function cannot create that authority.
func PublishReviewedDefectHoldout(ctx context.Context,
	publisher artifactjson.Publisher, draft TrainingDraftPrepared,
	prepared PreparedReviewedDefectHoldout) (v2input.Ref, error) {
	if publisher.Repository == nil || publisher.Objects == nil ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil ||
		draft.ActorID == uuid.Nil || draft.AuthSessionID == uuid.Nil ||
		prepared.Document.TenantID != draft.TenantID ||
		prepared.Document.EventID == uuid.Nil ||
		prepared.Document.ReviewID == uuid.Nil {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	ref, err := ReviewedDefectHoldoutRef(draft.RunID,
		prepared.Document.EventID, prepared.SHA256)
	if err != nil {
		return v2input.Ref{}, err
	}
	expected := DefectHoldoutExpected{TenantID: draft.TenantID,
		EventID:        prepared.Document.EventID,
		AssetVersionID: prepared.Document.AssetVersionID,
		ReviewID:       prepared.Document.ReviewID,
		RunID:          prepared.Document.RunID,
		ActorID:        prepared.Document.ActorID,
		DecisionHash:   prepared.Document.DecisionHash,
		Image:          prepared.Document.Image,
		Protection:     prepared.Document.Protection,
		Params:         prepared.Document.Params}
	if _, err := DecodeFrozenDefectHoldoutEvidence(
		prepared.Body, ref, expected); err != nil {
		return v2input.Ref{}, err
	}
	matches := 0
	for _, item := range draft.Manifest.Examples {
		if item.EventID != prepared.Document.EventID {
			continue
		}
		if item.RenderCase.DefectEvidence == nil ||
			*item.RenderCase.DefectEvidence != ref ||
			item.RenderCase.Binding.AssetVersionID !=
				prepared.Document.AssetVersionID ||
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
		Owner: prepared.Document.EventID, Purpose: "v23_defect_holdout",
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
