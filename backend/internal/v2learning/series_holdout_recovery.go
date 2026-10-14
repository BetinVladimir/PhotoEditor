package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type seriesHoldoutRecoveryItem struct {
	EventID uuid.UUID `json:"event_id"`
	BodyJCS string    `json:"body_jcs"`
}

// AttachSeriesHoldoutRecovery pins the exact publication bytes before the
// draft receipt is saved. The same event may never acquire different bytes on
// a retry, even if the underlying Apply source later changes.
func AttachSeriesHoldoutRecovery(draft TrainingDraftPrepared,
	prepared map[uuid.UUID]PreparedSeriesHoldout) (TrainingDraftPrepared, error) {
	if len(prepared) > len(draft.Manifest.Examples) || len(prepared) > 5000 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	items := make([]seriesHoldoutRecoveryItem, 0, len(prepared))
	for _, example := range draft.Manifest.Examples {
		item, exists := prepared[example.EventID]
		caseRef := example.RenderCase
		if caseRef.SeriesEvidence == nil {
			if exists {
				return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists || caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil ||
			caseRef.DetailEvidence != nil || item.Document.TenantID != draft.TenantID ||
			item.Document.EventID != example.EventID ||
			item.Document.AssetVersionID != caseRef.Binding.AssetVersionID ||
			item.Document.SourceImage != caseRef.WorkingImage {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		ref, err := SeriesHoldoutRef(draft.RunID, example.EventID, item.SHA256)
		if err != nil || ref != *caseRef.SeriesEvidence {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		if _, err := DecodeFrozenSeriesHoldoutEvidence(item.Body, ref,
			item.Document); err != nil {
			return TrainingDraftPrepared{}, err
		}
		items = append(items, seriesHoldoutRecoveryItem{
			EventID: example.EventID, BodyJCS: string(item.Body)})
	}
	if len(items) != len(prepared) {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	if len(items) == 0 {
		return draft, nil
	}
	itemsBody, err := canonical.Marshal(items)
	if err != nil || len(itemsBody) > 16777216 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	var recovery trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil ||
		recovery.SeriesJCS != "" {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	recovery.SeriesJCS = string(itemsBody)
	draft.RecoveryBody, err = canonical.Marshal(recovery)
	if err != nil || len(draft.RecoveryBody) > 33554432 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	return draft, nil
}

func seriesHoldoutFromRecovery(draft TrainingDraftPrepared,
	raw []byte) (map[uuid.UUID]PreparedSeriesHoldout, error) {
	result := make(map[uuid.UUID]PreparedSeriesHoldout)
	if len(raw) == 0 {
		for _, example := range draft.Manifest.Examples {
			if example.RenderCase.SeriesEvidence != nil {
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
	var items []seriesHoldoutRecoveryItem
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
		caseRef := example.RenderCase
		if caseRef.SeriesEvidence == nil {
			if exists {
				return nil, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists || caseRef.DefectEvidence != nil ||
			caseRef.DustEvidence != nil || caseRef.DetailEvidence != nil {
			return nil, ErrInvalidTrainingDraft
		}
		var parsed FrozenSeriesHoldoutEvidence
		if json.Unmarshal(body, &parsed) != nil ||
			parsed.TenantID != draft.TenantID ||
			parsed.EventID != example.EventID ||
			parsed.AssetVersionID != caseRef.Binding.AssetVersionID ||
			parsed.SourceImage != caseRef.WorkingImage {
			return nil, ErrInvalidTrainingDraft
		}
		stable, err := SeriesHoldoutRef(draft.RunID, example.EventID,
			caseRef.SeriesEvidence.SHA256)
		if err != nil || stable != *caseRef.SeriesEvidence {
			return nil, ErrInvalidTrainingDraft
		}
		frozen, err := DecodeFrozenSeriesHoldoutEvidence(body,
			*caseRef.SeriesEvidence, parsed)
		if err != nil {
			return nil, err
		}
		result[example.EventID] = PreparedSeriesHoldout{
			Document: frozen, Body: body,
			SHA256: caseRef.SeriesEvidence.SHA256}
	}
	if len(result) != len(items) {
		return nil, ErrInvalidTrainingDraft
	}
	return result, nil
}
