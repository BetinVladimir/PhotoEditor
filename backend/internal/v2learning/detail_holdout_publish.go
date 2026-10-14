package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// PublishPortraitDetailHoldout publishes the exact prepared producer evidence.
// The SQL reservation and completion each recheck the event's immutable Apply
// producer link, current consent, and the saved training manifest reference.
func PublishPortraitDetailHoldout(ctx context.Context,
	publisher artifactjson.Publisher, draft TrainingDraftPrepared,
	prepared PreparedPortraitDetailHoldout) (v2input.Ref, error) {
	if publisher.Repository == nil || publisher.Objects == nil ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil ||
		draft.ProjectID == uuid.Nil || draft.ActorID == uuid.Nil ||
		draft.AuthSessionID == uuid.Nil ||
		prepared.Document.TenantID != draft.TenantID ||
		prepared.Document.EventID == uuid.Nil {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	ref, err := PortraitDetailHoldoutRef(draft.RunID,
		prepared.Document.EventID, prepared.SHA256)
	if err != nil {
		return v2input.Ref{}, err
	}
	expected := PortraitDetailHoldoutOrigin{
		TenantID:       prepared.Document.TenantID,
		EventID:        prepared.Document.EventID,
		AssetVersionID: prepared.Document.AssetVersionID,
		RunID:          prepared.Document.RunID, TaskID: prepared.Document.TaskID,
		ResultID: prepared.Document.ResultID, AttemptID: prepared.Document.AttemptID,
		Source: prepared.Document.Source, Image: prepared.Document.Image,
		Protection: prepared.Document.Protection, Params: prepared.Document.Params,
		Resolved:      prepared.Document.Resolved,
		MaskArtifacts: prepared.Document.MaskArtifacts,
	}
	if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(
		prepared.Body, ref, expected); err != nil {
		return v2input.Ref{}, err
	}
	matches := 0
	for _, item := range draft.Manifest.Examples {
		if item.EventID != prepared.Document.EventID {
			continue
		}
		caseRef := item.RenderCase
		if caseRef.DetailEvidence == nil || *caseRef.DetailEvidence != ref ||
			caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil ||
			caseRef.Binding.AssetVersionID != prepared.Document.AssetVersionID ||
			caseRef.WorkingImage != prepared.Document.Source ||
			caseRef.Protected != prepared.Document.Protection {
			return v2input.Ref{}, ErrInvalidTrainingDraft
		}
		matches++
	}
	if matches != 1 {
		return v2input.Ref{}, ErrInvalidTrainingDraft
	}
	got, err := publisher.Publish(ctx, artifactjson.Scope{
		Tenant: draft.TenantID, Run: draft.RunID,
		Owner: prepared.Document.EventID, Purpose: "v23_detail_holdout",
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
