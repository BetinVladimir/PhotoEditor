package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// PublishSeriesHoldout publishes only the exact event-owned recovery body.
// Reserve and Complete each repeat the SQL Apply source and manifest binding.
// This does not admit Fit or Evaluate while the series freeze gate is active.
func PublishSeriesHoldout(ctx context.Context,
	publisher artifactjson.Publisher, draft TrainingDraftPrepared,
	prepared PreparedSeriesHoldout) (v2input.Ref, error) {
	if publisher.Repository == nil || publisher.Objects == nil ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil ||
		draft.ProjectID == uuid.Nil || draft.ActorID == uuid.Nil ||
		draft.AuthSessionID == uuid.Nil ||
		prepared.Document.TenantID != draft.TenantID ||
		prepared.Document.EventID == uuid.Nil {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	ref, err := SeriesHoldoutRef(draft.RunID,
		prepared.Document.EventID, prepared.SHA256)
	if err != nil {
		return v2input.Ref{}, err
	}
	if _, err := DecodeFrozenSeriesHoldoutEvidence(prepared.Body,
		ref, prepared.Document); err != nil {
		return v2input.Ref{}, err
	}
	matches := 0
	for _, example := range draft.Manifest.Examples {
		if example.EventID != prepared.Document.EventID {
			continue
		}
		caseRef := example.RenderCase
		if caseRef.SeriesEvidence == nil || *caseRef.SeriesEvidence != ref ||
			caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil ||
			caseRef.DetailEvidence != nil ||
			caseRef.Binding.AssetVersionID != prepared.Document.AssetVersionID ||
			caseRef.WorkingImage != prepared.Document.SourceImage {
			return v2input.Ref{}, ErrInvalidTrainingDraft
		}
		matches++
	}
	if matches != 1 {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	got, err := publisher.Publish(ctx, artifactjson.Scope{
		Tenant: draft.TenantID, Run: draft.RunID,
		Owner: prepared.Document.EventID, Purpose: "v23_series_holdout",
		Project: draft.ProjectID, Actor: draft.ActorID,
		AuthSession: draft.AuthSessionID}, prepared.Body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if got.ID != ref.ArtifactID || got.SHA256 != ref.SHA256 {
		return v2input.Ref{}, ErrTrainingDraftConflict
	}
	return ref, nil
}
