package v2learning

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ValidateFrozenPortraitDetailCase checks the immutable manifest binding.
// A fenced SQL reader must independently verify the current event, consent,
// producer and exact document object version before Evaluate may use it.
func ValidateFrozenPortraitDetailCase(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	evidence FrozenPortraitDetailHoldoutEvidence) error {
	if !validLearningEvaluateLease(lease) || eventID == uuid.Nil ||
		caseRef.DetailEvidence == nil || caseRef.DefectEvidence != nil ||
		caseRef.DustEvidence != nil ||
		!validManifestRef(*caseRef.DetailEvidence) ||
		evidence.TenantID != lease.TenantID ||
		evidence.EventID != eventID ||
		evidence.AssetVersionID != caseRef.Binding.AssetVersionID ||
		evidence.Source != caseRef.WorkingImage ||
		evidence.Source != caseRef.Binding.Source ||
		evidence.Protection != caseRef.Protected ||
		evidence.Image == evidence.Source ||
		!validPortraitDetailHoldoutEvidence(evidence) {
		return ErrInvalidTrainingDraft
	}
	stable, err := PortraitDetailHoldoutRef(lease.RunID,
		eventID, caseRef.DetailEvidence.SHA256)
	if err != nil || stable != *caseRef.DetailEvidence {
		return ErrInvalidTrainingDraft
	}
	return nil
}

// validateFrozenDetailEvaluateInputs prevents a decoded evidence document from
// introducing an unregistered or substituted nested input into Evaluate.
// The SQL reader still owns physical version and live producer authority.
func validateFrozenDetailEvaluateInputs(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	proof FrozenEvaluateDetailInputs) error {
	if err := ValidateFrozenPortraitDetailCase(lease, eventID, caseRef,
		proof.Evidence); err != nil {
		return err
	}
	expected := []v2input.Entry{
		{Ref: *caseRef.DetailEvidence, Contract: "FrozenPortraitDetailHoldoutEvidence"},
		{Ref: proof.Evidence.Image, Contract: "WorkingImage"},
		{Ref: proof.Evidence.Params, Contract: "PortraitParams"},
		{Ref: proof.Evidence.Resolved, Contract: "ResolvedStyle"},
	}
	for _, mask := range proof.Evidence.MaskArtifacts {
		expected = append(expected, v2input.Entry{Ref: mask, Contract: "NpyArray"})
	}
	if len(proof.Entries) != len(expected) {
		return ErrInvalidTrainingDraft
	}
	seen := make(map[uuid.UUID]bool, len(expected))
	for index, entry := range proof.Entries {
		if entry != expected[index] || !validManifestRef(entry.Ref) ||
			seen[entry.Ref.ArtifactID] {
			return ErrInvalidTrainingDraft
		}
		seen[entry.Ref.ArtifactID] = true
	}
	return nil
}
