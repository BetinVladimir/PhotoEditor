package v2learning

import "github.com/google/uuid"

// ValidateFrozenDustHoldoutCase binds a decoded document to the exact frozen
// example and Evaluate lease. SQL must still prove live review/consent and
// the registered object version before and after physical IO.
func ValidateFrozenDustHoldoutCase(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	evidence FrozenDustHoldoutEvidence) error {
	if !validLearningEvaluateLease(lease) || eventID == uuid.Nil ||
		caseRef.DustEvidence == nil || caseRef.DefectEvidence != nil ||
		!validManifestRef(*caseRef.DustEvidence) ||
		evidence.SchemaVersion != 1 || evidence.TenantID != lease.TenantID ||
		evidence.EventID != eventID ||
		evidence.AssetVersionID != caseRef.Binding.AssetVersionID ||
		evidence.Image != caseRef.WorkingImage ||
		evidence.Image != caseRef.Binding.Source ||
		evidence.Protection != caseRef.Protected ||
		evidence.StageImage == evidence.Image ||
		evidence.Decision != "accept" || evidence.Route != "repair" ||
		evidence.ReviewID == uuid.Nil || evidence.RunID == uuid.Nil ||
		evidence.ActorID == uuid.Nil ||
		!validSHA(evidence.DecisionHash) ||
		!validManifestRef(evidence.StageImage) ||
		!validManifestRef(evidence.Proposal) ||
		!validManifestRef(evidence.Params) {
		return ErrInvalidTrainingDraft
	}
	decision := DustHoldoutDecision{TenantID: evidence.TenantID,
		ReviewID: evidence.ReviewID, RunID: evidence.RunID,
		ActorID: evidence.ActorID, Decision: evidence.Decision,
		Route: evidence.Route, DecisionHash: evidence.DecisionHash,
		Revision: evidence.Selection.Revision, Selection: evidence.Selection}
	_, _, sha, err := CompileFrozenDustHoldoutEvidence(decision, eventID,
		evidence.AssetVersionID, evidence.Image, evidence.StageImage,
		evidence.Protection, evidence.Proposal, evidence.Params)
	if err != nil || sha != caseRef.DustEvidence.SHA256 {
		return ErrInvalidTrainingDraft
	}
	stable, err := ReviewedDustHoldoutRef(lease.RunID, eventID, sha)
	if err != nil || stable != *caseRef.DustEvidence {
		return ErrInvalidTrainingDraft
	}
	return nil
}
