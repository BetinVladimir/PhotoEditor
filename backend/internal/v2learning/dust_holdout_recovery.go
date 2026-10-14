package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type dustHoldoutRecoveryItem struct {
	EventID uuid.UUID `json:"event_id"`
	BodyJCS string    `json:"body_jcs"`
}

func dustHoldoutExpected(item FrozenDustHoldoutEvidence) DustHoldoutExpected {
	return DustHoldoutExpected{TenantID: item.TenantID,
		EventID: item.EventID, AssetVersionID: item.AssetVersionID,
		ReviewID: item.ReviewID, RunID: item.RunID, ActorID: item.ActorID,
		DecisionHash: item.DecisionHash, Image: item.Image,
		StageImage: item.StageImage, Protection: item.Protection,
		Proposal: item.Proposal, Params: item.Params}
}

// AttachReviewedDustRecovery freezes publication bytes before BeginTrainingDraft.
func AttachReviewedDustRecovery(draft TrainingDraftPrepared,
	prepared map[uuid.UUID]PreparedReviewedDustHoldout) (TrainingDraftPrepared, error) {
	if len(prepared) > len(draft.Manifest.Examples) || len(prepared) > 5000 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	items := make([]dustHoldoutRecoveryItem, 0, len(prepared))
	for _, example := range draft.Manifest.Examples {
		item, exists := prepared[example.EventID]
		if example.RenderCase.DustEvidence == nil {
			if exists {
				return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists || example.RenderCase.DefectEvidence != nil ||
			item.Document.TenantID != draft.TenantID ||
			item.Document.EventID != example.EventID ||
			item.Document.AssetVersionID != example.RenderCase.Binding.AssetVersionID ||
			item.Document.Image != example.RenderCase.WorkingImage ||
			item.Document.Protection != example.RenderCase.Protected {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		ref, err := ReviewedDustHoldoutRef(draft.RunID, example.EventID,
			item.SHA256)
		if err != nil || ref != *example.RenderCase.DustEvidence {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		if _, err := DecodeFrozenDustHoldoutEvidence(item.Body, ref,
			dustHoldoutExpected(item.Document)); err != nil {
			return TrainingDraftPrepared{}, err
		}
		items = append(items, dustHoldoutRecoveryItem{EventID: example.EventID,
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
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil ||
		recovery.DustJCS != "" {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	recovery.DustJCS = string(itemsBody)
	draft.RecoveryBody, err = canonical.Marshal(recovery)
	if err != nil || len(draft.RecoveryBody) > 33554432 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	return draft, nil
}

func reviewedDustFromRecovery(draft TrainingDraftPrepared,
	raw []byte) (map[uuid.UUID]PreparedReviewedDustHoldout, error) {
	result := make(map[uuid.UUID]PreparedReviewedDustHoldout)
	if len(raw) == 0 {
		for _, example := range draft.Manifest.Examples {
			if example.RenderCase.DustEvidence != nil {
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
	var items []dustHoldoutRecoveryItem
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
		if example.RenderCase.DustEvidence == nil {
			if exists {
				return nil, ErrInvalidTrainingDraft
			}
			continue
		}
		if !exists || example.RenderCase.DefectEvidence != nil {
			return nil, ErrInvalidTrainingDraft
		}
		var parsed FrozenDustHoldoutEvidence
		if json.Unmarshal(body, &parsed) != nil ||
			parsed.TenantID != draft.TenantID ||
			parsed.EventID != example.EventID ||
			parsed.AssetVersionID != example.RenderCase.Binding.AssetVersionID ||
			parsed.Image != example.RenderCase.WorkingImage ||
			parsed.Protection != example.RenderCase.Protected {
			return nil, ErrInvalidTrainingDraft
		}
		stable, err := ReviewedDustHoldoutRef(draft.RunID,
			example.EventID, example.RenderCase.DustEvidence.SHA256)
		if err != nil || stable != *example.RenderCase.DustEvidence {
			return nil, ErrInvalidTrainingDraft
		}
		frozen, err := DecodeFrozenDustHoldoutEvidence(body,
			*example.RenderCase.DustEvidence, dustHoldoutExpected(parsed))
		if err != nil {
			return nil, err
		}
		result[example.EventID] = PreparedReviewedDustHoldout{
			Document: frozen, Body: body,
			SHA256: example.RenderCase.DustEvidence.SHA256}
	}
	if len(result) != len(items) {
		return nil, ErrInvalidTrainingDraft
	}
	return result, nil
}
