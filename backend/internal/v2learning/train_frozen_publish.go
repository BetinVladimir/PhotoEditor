package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// PublishTrainingFrozenPair runs after BeginTrainingDraft has committed its
// recovery manifest and before publishing that manifest or sealing Fit.
// Stable IDs and recovery SHA bind each publication to one selected event.
func PublishTrainingFrozenPair(ctx context.Context,
	publisher artifactjson.Publisher, draft TrainingDraftPrepared,
	eventID uuid.UUID, compiled CompiledTrainingRecipes) (
	v2input.Ref, v2input.Ref, error) {
	var empty v2input.Ref
	if publisher.Repository == nil || publisher.Objects == nil {
		return empty, empty, ErrInvalidTrainingDraft
	}
	base, approved, err := ValidateTrainingFrozenPair(draft, eventID,
		compiled)
	if err != nil {
		return empty, empty, err
	}
	for _, item := range []struct {
		side     string
		body     []byte
		expected v2input.Ref
	}{{"base", compiled.BaseBody, base},
		{"approved", compiled.ApprovedBody, approved}} {
		published, err := publisher.Publish(ctx, artifactjson.Scope{
			Tenant: draft.TenantID, Run: draft.RunID, Owner: eventID,
			Purpose: "v23_frozen_" + item.side}, item.body)
		if err != nil {
			return empty, empty, err
		}
		if published.ID != item.expected.ArtifactID ||
			published.SHA256 != item.expected.SHA256 {
			return empty, empty, ErrTrainingDraftConflict
		}
	}
	return base, approved, nil
}

// ValidateTrainingFrozenPair checks every byte and manifest binding before
// admission creates a draft. Publication can then be retried independently.
func ValidateTrainingFrozenPair(draft TrainingDraftPrepared,
	eventID uuid.UUID, compiled CompiledTrainingRecipes) (
	v2input.Ref, v2input.Ref, error) {
	var empty v2input.Ref
	if draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil ||
		eventID == uuid.Nil || len(compiled.BaseBody) < 2 ||
		len(compiled.ApprovedBody) < 2 {
		return empty, empty, ErrInvalidTrainingDraft
	}
	base, approved, err := compiled.Refs(draft.RunID, eventID)
	if err != nil {
		return empty, empty, err
	}
	baseRecipe, err := DecodeFrozenRecipe(compiled.BaseBody)
	baseSum := sha256.Sum256(compiled.BaseBody)
	approvedSum := sha256.Sum256(compiled.ApprovedBody)
	if err != nil || baseRecipe != compiled.Base ||
		hex.EncodeToString(baseSum[:]) != compiled.BaseSHA ||
		hex.EncodeToString(approvedSum[:]) != compiled.ApprovedSHA {
		return empty, empty, ErrInvalidTrainingDraft
	}
	approvedRecipe, err := DecodeFrozenRecipe(compiled.ApprovedBody)
	if err != nil || approvedRecipe != compiled.Approved ||
		baseRecipe.Source != approvedRecipe.Source ||
		baseRecipe.BaseStyle != approvedRecipe.BaseStyle ||
		baseRecipe.ICCLockSHA != draft.Manifest.ICCLockSHA ||
		approvedRecipe.ICCLockSHA != draft.Manifest.ICCLockSHA ||
		baseRecipe.ModelLockSHA != draft.Manifest.ModelLockSHA ||
		approvedRecipe.ModelLockSHA != draft.Manifest.ModelLockSHA {
		return empty, empty, ErrInvalidTrainingDraft
	}
	matches := 0
	for _, item := range draft.Manifest.Examples {
		if item.EventID == eventID {
			if item.RenderCase.BaseRecipe != base ||
				item.RenderCase.ApprovedRecipe != approved ||
				item.RenderCase.Binding != baseRecipe.Source {
				return empty, empty, ErrInvalidTrainingDraft
			}
			matches++
		}
	}
	if matches != 1 {
		return empty, empty, ErrInvalidTrainingDraft
	}
	return base, approved, nil
}
