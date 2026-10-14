package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type detailHoldoutRecoveryItem struct {
	EventID uuid.UUID `json:"event_id"`
	BodyJCS string    `json:"body_jcs"`
}

func detailHoldoutExpected(item FrozenPortraitDetailHoldoutEvidence) PortraitDetailHoldoutOrigin {
	return PortraitDetailHoldoutOrigin{
		TenantID: item.TenantID, EventID: item.EventID,
		AssetVersionID: item.AssetVersionID,
		RunID:          item.RunID, TaskID: item.TaskID,
		ResultID: item.ResultID, AttemptID: item.AttemptID,
		Source: item.Source, Image: item.Image,
		Protection: item.Protection, Params: item.Params,
		Resolved: item.Resolved, MaskArtifacts: item.MaskArtifacts,
	}
}

// AttachPortraitDetailRecovery pins exact publication bytes before the draft
// receipt is saved. Retry never reconstructs producer evidence from live rows.
func AttachPortraitDetailRecovery(draft TrainingDraftPrepared,
	prepared map[uuid.UUID]PreparedPortraitDetailHoldout) (TrainingDraftPrepared, error) {
	if len(prepared) > len(draft.Manifest.Examples) || len(prepared) > 5000 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	items := make([]detailHoldoutRecoveryItem, 0, len(prepared))
	for _, example := range draft.Manifest.Examples {
		item, exists := prepared[example.EventID]
		caseRef := example.RenderCase
		if caseRef.DetailEvidence == nil {
			if exists {
				return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists || caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil ||
			item.Document.TenantID != draft.TenantID ||
			item.Document.EventID != example.EventID ||
			item.Document.AssetVersionID != caseRef.Binding.AssetVersionID ||
			item.Document.Source != caseRef.WorkingImage ||
			item.Document.Protection != caseRef.Protected {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		ref, err := PortraitDetailHoldoutRef(draft.RunID, example.EventID,
			item.SHA256)
		if err != nil || ref != *caseRef.DetailEvidence {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(
			item.Body, ref, detailHoldoutExpected(item.Document)); err != nil {
			return TrainingDraftPrepared{}, err
		}
		items = append(items, detailHoldoutRecoveryItem{EventID: example.EventID,
			BodyJCS: string(item.Body)})
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
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil || recovery.DetailJCS != "" {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	recovery.DetailJCS = string(itemsBody)
	draft.RecoveryBody, err = canonical.Marshal(recovery)
	if err != nil || len(draft.RecoveryBody) > 33554432 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	return draft, nil
}

func portraitDetailFromRecovery(draft TrainingDraftPrepared,
	raw []byte) (map[uuid.UUID]PreparedPortraitDetailHoldout, error) {
	result := make(map[uuid.UUID]PreparedPortraitDetailHoldout)
	if len(raw) == 0 {
		for _, example := range draft.Manifest.Examples {
			if example.RenderCase.DetailEvidence != nil {
				return nil, ErrInvalidTrainingDraft
			}
		}
		return result, nil
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || len(raw) > 16777216 || !bytes.Equal(canonicalBody, raw) {
		return nil, ErrInvalidTrainingDraft
	}
	var items []detailHoldoutRecoveryItem
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
		if caseRef.DetailEvidence == nil {
			if exists {
				return nil, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists || caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil {
			return nil, ErrInvalidTrainingDraft
		}
		var parsed FrozenPortraitDetailHoldoutEvidence
		if json.Unmarshal(body, &parsed) != nil ||
			parsed.TenantID != draft.TenantID || parsed.EventID != example.EventID ||
			parsed.AssetVersionID != caseRef.Binding.AssetVersionID ||
			parsed.Source != caseRef.WorkingImage ||
			parsed.Protection != caseRef.Protected {
			return nil, ErrInvalidTrainingDraft
		}
		stable, err := PortraitDetailHoldoutRef(draft.RunID, example.EventID,
			caseRef.DetailEvidence.SHA256)
		if err != nil || stable != *caseRef.DetailEvidence {
			return nil, ErrInvalidTrainingDraft
		}
		frozen, err := DecodeFrozenPortraitDetailHoldoutEvidence(body,
			*caseRef.DetailEvidence, detailHoldoutExpected(parsed))
		if err != nil {
			return nil, err
		}
		result[example.EventID] = PreparedPortraitDetailHoldout{
			Document: frozen, Body: body, SHA256: caseRef.DetailEvidence.SHA256}
	}
	if len(result) != len(items) {
		return nil, ErrInvalidTrainingDraft
	}
	return result, nil
}
