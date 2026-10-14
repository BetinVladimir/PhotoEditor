package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// DefectHoldoutExpected is supplied by the current SQL snapshot/lease source,
// not by the saved document or an HTTP caller.
type DefectHoldoutExpected struct {
	TenantID       uuid.UUID
	EventID        uuid.UUID
	AssetVersionID uuid.UUID
	ReviewID       uuid.UUID
	RunID          uuid.UUID
	ActorID        uuid.UUID
	DecisionHash   string
	Image          v2input.Ref
	Protection     v2input.Ref
	Params         v2input.Ref
}

// DecodeFrozenDefectHoldoutEvidence requires exact registered object bytes.
// It is a document check; live review, artifact-version and consent authority
// remain obligations of the scoped SQL reader before and after object IO.
func DecodeFrozenDefectHoldoutEvidence(raw []byte, ref v2input.Ref, expected DefectHoldoutExpected) (FrozenDefectHoldoutEvidence, error) {
	if len(raw) < 2 || len(raw) > 1<<20 || !validManifestRef(ref) ||
		expected.TenantID == uuid.Nil || expected.EventID == uuid.Nil || expected.AssetVersionID == uuid.Nil || expected.ReviewID == uuid.Nil || expected.RunID == uuid.Nil || expected.ActorID == uuid.Nil ||
		!validSHA(expected.DecisionHash) || !validManifestRef(expected.Image) ||
		!validManifestRef(expected.Protection) || !validManifestRef(expected.Params) {
		return FrozenDefectHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	var parsed FrozenDefectHoldoutEvidence
	if json.Unmarshal(raw, &parsed) != nil || parsed.SchemaVersion != 1 ||
		parsed.TenantID != expected.TenantID || parsed.EventID != expected.EventID || parsed.AssetVersionID != expected.AssetVersionID || parsed.ReviewID != expected.ReviewID ||
		parsed.RunID != expected.RunID || parsed.ActorID != expected.ActorID ||
		parsed.DecisionHash != expected.DecisionHash || parsed.Image != expected.Image ||
		parsed.Protection != expected.Protection || parsed.Params != expected.Params {
		return FrozenDefectHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	decision := DefectHoldoutDecision{TenantID: parsed.TenantID, ReviewID: parsed.ReviewID,
		RunID: parsed.RunID, ActorID: parsed.ActorID, Decision: parsed.Decision,
		Route: parsed.Route, DecisionHash: parsed.DecisionHash,
		Revision: parsed.Revision, Selection: parsed.Selection}
	frozen, canonicalBody, sha, err := CompileFrozenDefectHoldoutEvidence(decision, parsed.EventID, parsed.AssetVersionID, parsed.Image, parsed.Protection, parsed.Params)
	if err != nil || sha != ref.SHA256 || !bytes.Equal(canonicalBody, raw) {
		return FrozenDefectHoldoutEvidence{}, ErrInvalidTrainingDraft
	}
	return frozen, nil
}

// ValidateFrozenDefectHoldoutCase binds a decoded, SQL-authorized document to
// the exact holdout example. It does not replace the live lease/review check.
func ValidateFrozenDefectHoldoutCase(lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase, evidence FrozenDefectHoldoutEvidence) error {
	if !validLearningEvaluateLease(lease) || caseRef.DefectEvidence == nil ||
		!validManifestRef(*caseRef.DefectEvidence) || eventID == uuid.Nil ||
		evidence.SchemaVersion != 1 || evidence.TenantID != lease.TenantID ||
		evidence.EventID != eventID || evidence.AssetVersionID != caseRef.Binding.AssetVersionID ||
		evidence.Image != caseRef.WorkingImage || evidence.Protection != caseRef.Protected ||
		evidence.Decision != "accept" || evidence.Route != "repair" ||
		!validManifestRef(evidence.Params) || evidence.ReviewID == uuid.Nil ||
		evidence.ActorID == uuid.Nil || evidence.RunID == uuid.Nil ||
		!validSHA(evidence.DecisionHash) {
		return ErrInvalidTrainingDraft
	}
	return nil
}
