package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type trainingFrozenRecoveryItem struct {
	EventID     uuid.UUID `json:"event_id"`
	BaseJCS     string    `json:"base_jcs"`
	ApprovedJCS string    `json:"approved_jcs"`
}

// AttachTrainingFrozenRecovery stores the exact small FrozenRecipe documents
// in the pending idempotency record before any object IO. Seal replaces this
// record with the ticket; retry can republish any missing run-scoped object.
func AttachTrainingFrozenRecovery(draft TrainingDraftPrepared,
	frozen map[uuid.UUID]CompiledTrainingRecipes) (TrainingDraftPrepared, error) {
	if len(frozen) != len(draft.Manifest.Examples) ||
		len(frozen) < 40 || len(frozen) > 5000 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	items := make([]trainingFrozenRecoveryItem, 0, len(frozen))
	for _, example := range draft.Manifest.Examples {
		pair, okay := frozen[example.EventID]
		if !okay {
			return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
		}
		if _, _, err := ValidateTrainingFrozenPair(draft,
			example.EventID, pair); err != nil {
			return TrainingDraftPrepared{}, err
		}
		items = append(items, trainingFrozenRecoveryItem{
			EventID: example.EventID, BaseJCS: string(pair.BaseBody),
			ApprovedJCS: string(pair.ApprovedBody)})
	}
	itemsBody, err := canonical.Marshal(items)
	if err != nil || len(itemsBody) > 16777216 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	var recovery trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil ||
		recovery.FrozenJCS != "" {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	recovery.FrozenJCS = string(itemsBody)
	draft.RecoveryBody, err = canonical.Marshal(recovery)
	if err != nil || len(draft.RecoveryBody) > 33554432 {
		return TrainingDraftPrepared{}, ErrInvalidTrainingDraft
	}
	return draft, nil
}

func frozenRecipesFromRecovery(draft TrainingDraftPrepared,
	raw []byte) (map[uuid.UUID]CompiledTrainingRecipes, error) {
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) ||
		len(raw) > 16777216 {
		return nil, ErrInvalidTrainingDraft
	}
	var items []trainingFrozenRecoveryItem
	if json.Unmarshal(raw, &items) != nil ||
		len(items) != len(draft.Manifest.Examples) {
		return nil, ErrInvalidTrainingDraft
	}
	result := make(map[uuid.UUID]CompiledTrainingRecipes, len(items))
	for i, item := range items {
		example := draft.Manifest.Examples[i]
		if item.EventID != example.EventID ||
			result[item.EventID].BaseBody != nil {
			return nil, ErrInvalidTrainingDraft
		}
		baseBody, approvedBody := []byte(item.BaseJCS),
			[]byte(item.ApprovedJCS)
		base, err := DecodeFrozenRecipe(baseBody)
		if err != nil {
			return nil, err
		}
		approved, err := DecodeFrozenRecipe(approvedBody)
		if err != nil {
			return nil, err
		}
		pair := CompiledTrainingRecipes{Base: base, Approved: approved,
			BaseBody: baseBody, ApprovedBody: approvedBody,
			BaseSHA:     example.RenderCase.BaseRecipe.SHA256,
			ApprovedSHA: example.RenderCase.ApprovedRecipe.SHA256}
		if _, _, err := ValidateTrainingFrozenPair(draft,
			item.EventID, pair); err != nil {
			return nil, err
		}
		result[item.EventID] = pair
	}
	return result, nil
}
