package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type defectHoldoutRecoveryItem struct {
	EventID uuid.UUID `json:"event_id"`
	BodyJCS string    `json:"body_jcs"`
}

// AttachReviewedDefectRecovery persists exact publication bytes in the
// pending draft receipt. It must run before BeginTrainingDraft; stage can then
// retry every document after a lost object-store or SQL acknowledgement.
func AttachReviewedDefectRecovery(draft TrainingDraftPrepared,
	prepared map[uuid.UUID]PreparedReviewedDefectHoldout) (TrainingDraftPrepared, error) {
	if len(prepared) == 0 {
		for _, example := range draft.Manifest.Examples {
			if example.RenderCase.DefectEvidence != nil {
				return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
			}
		}
		return draft, nil
	}
	if len(prepared) > len(draft.Manifest.Examples) || len(prepared) > 5000 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	items := make([]defectHoldoutRecoveryItem, 0, len(prepared))
	for _, example := range draft.Manifest.Examples {
		if example.RenderCase.DefectEvidence == nil {
			if _, exists := prepared[example.EventID]; exists {
				return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
			}
			continue
		}
		item, exists := prepared[example.EventID]
		if !exists || item.Document.TenantID != draft.TenantID ||
			item.Document.EventID != example.EventID ||
			item.Document.AssetVersionID != example.RenderCase.Binding.AssetVersionID ||
			item.Document.Image != example.RenderCase.WorkingImage ||
			item.Document.Protection != example.RenderCase.Protected {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		ref, err := ReviewedDefectHoldoutRef(draft.RunID, example.EventID,
			item.SHA256)
		if err != nil || ref != *example.RenderCase.DefectEvidence {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		expected := defectHoldoutExpected(item.Document)
		if _, err := DecodeFrozenDefectHoldoutEvidence(item.Body, ref,
			expected); err != nil {
			return TrainingDraftPrepared{}, err
		}
		items = append(items, defectHoldoutRecoveryItem{EventID: example.EventID,
			BodyJCS: string(item.Body)})
	}
	if len(items) != len(prepared) {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	itemsBody, err := canonical.Marshal(items)
	if err != nil || len(itemsBody) > 16777216 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	var recovery trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil ||
		recovery.DefectJCS != "" {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	recovery.DefectJCS = string(itemsBody)
	draft.RecoveryBody, err = canonical.Marshal(recovery)
	if err != nil || len(draft.RecoveryBody) > 33554432 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	return draft, nil
}

func reviewedDefectsFromRecovery(draft TrainingDraftPrepared,
	raw []byte) (map[uuid.UUID]PreparedReviewedDefectHoldout, error) {
	result := make(map[uuid.UUID]PreparedReviewedDefectHoldout)
	if len(raw) == 0 {
		for _, example := range draft.Manifest.Examples {
			if example.RenderCase.DefectEvidence != nil {
				return nil, ErrInvalidTrainingDraft
			}
		}
		return result, nil
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || len(raw) > 16777216 ||
		!bytes.Equal(canonicalBody, raw) {
		return nil, ErrInvalidTrainingDraft
	}
	var items []defectHoldoutRecoveryItem
	if json.Unmarshal(raw, &items) != nil || len(items) > 5000 {
		return nil, ErrInvalidTrainingDraft
	}
	byEvent := make(map[uuid.UUID][]byte, len(items))
	for _, item := range items {
		if item.EventID == uuid.Nil || byEvent[item.EventID] != nil {
			return nil, ErrInvalidTrainingDraft
		}
		byEvent[item.EventID] = []byte(item.BodyJCS)
	}
	for _, example := range draft.Manifest.Examples {
		body, exists := byEvent[example.EventID]
		if example.RenderCase.DefectEvidence == nil {
			if exists {
				return nil, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists {
			return nil, ErrInvalidTrainingDraft
		}
		var parsed FrozenDefectHoldoutEvidence
		if json.Unmarshal(body, &parsed) != nil ||
			parsed.TenantID != draft.TenantID ||
			parsed.EventID != example.EventID ||
			parsed.AssetVersionID != example.RenderCase.Binding.AssetVersionID ||
			parsed.Image != example.RenderCase.WorkingImage ||
			parsed.Protection != example.RenderCase.Protected {
			return nil, ErrInvalidTrainingDraft
		}
		stable, err := ReviewedDefectHoldoutRef(draft.RunID,
			example.EventID, example.RenderCase.DefectEvidence.SHA256)
		if err != nil || stable != *example.RenderCase.DefectEvidence {
			return nil, ErrInvalidTrainingDraft
		}
		frozen, err := DecodeFrozenDefectHoldoutEvidence(body,
			*example.RenderCase.DefectEvidence, defectHoldoutExpected(parsed))
		if err != nil {
			return nil, err
		}
		result[example.EventID] = PreparedReviewedDefectHoldout{
			Document: frozen, Body: body,
			SHA256: example.RenderCase.DefectEvidence.SHA256}
	}
	if len(result) != len(items) {
		return nil, ErrInvalidTrainingDraft
	}
	return result, nil
}

func defectHoldoutExpected(item FrozenDefectHoldoutEvidence) DefectHoldoutExpected {
	return DefectHoldoutExpected{TenantID: item.TenantID,
		EventID: item.EventID, AssetVersionID: item.AssetVersionID,
		ReviewID: item.ReviewID, RunID: item.RunID,
		ActorID: item.ActorID, DecisionHash: item.DecisionHash,
		Image: item.Image, Protection: item.Protection, Params: item.Params}
}
