package v2learning

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// FrozenEvaluateSeriesInputs is a physically read, SQL-authorized snapshot.
// The bodies are retained so Evaluate can verify the aggregate behind every
// nested SeriesStats ref before sealing the worker input closure.
type FrozenEvaluateSeriesInputs struct {
	Evidence    FrozenSeriesHoldoutEvidence
	Entries     []v2input.Entry
	ParamsBody  []byte
	SeriesBody  []byte
	StatsBodies [][]byte
}

func ValidateFrozenSeriesCase(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	evidence FrozenSeriesHoldoutEvidence) error {
	if !validLearningEvaluateLease(lease) || eventID == uuid.Nil ||
		caseRef.SeriesEvidence == nil || caseRef.DefectEvidence != nil ||
		caseRef.DustEvidence != nil || caseRef.DetailEvidence != nil ||
		!validManifestRef(*caseRef.SeriesEvidence) ||
		!validFrozenSeriesHoldout(evidence) ||
		evidence.TenantID != lease.TenantID ||
		evidence.EventID != eventID ||
		evidence.AssetVersionID != caseRef.Binding.AssetVersionID ||
		evidence.SourceImage != caseRef.WorkingImage ||
		evidence.SourceImage != caseRef.Binding.Source {
		return ErrInvalidTrainingDraft
	}
	stable, err := SeriesHoldoutRef(lease.RunID,
		eventID, caseRef.SeriesEvidence.SHA256)
	if err != nil || stable != *caseRef.SeriesEvidence {
		return ErrInvalidTrainingDraft
	}
	return nil
}

func validateFrozenSeriesEvaluateInputs(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	proof FrozenEvaluateSeriesInputs) error {
	if err := ValidateFrozenSeriesCase(lease, eventID,
		caseRef, proof.Evidence); err != nil {
		return err
	}
	var params frozenSeriesParams
	if !decodePinnedSeriesJSON(proof.ParamsBody,
		proof.Evidence.Params.SHA256, &params) ||
		!validSeriesStatsRefs(params.Stats) ||
		len(proof.StatsBodies) != len(params.Stats) {
		return ErrInvalidTrainingDraft
	}
	if err := ValidateFrozenSeriesStats(context.Background(), proof.Evidence,
		proof.ParamsBody, proof.SeriesBody, proof.StatsBodies); err != nil {
		return err
	}
	expected := []v2input.Entry{
		{Ref: *caseRef.SeriesEvidence, Contract: "FrozenSeriesHoldoutEvidence"},
		{Ref: proof.Evidence.StageImage, Contract: "WorkingImage"},
		{Ref: proof.Evidence.Params, Contract: "SeriesParams"},
		{Ref: proof.Evidence.Series, Contract: "SeriesCorrections"},
	}
	for _, ref := range params.Stats {
		expected = append(expected,
			v2input.Entry{Ref: ref, Contract: "SeriesStats"})
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
