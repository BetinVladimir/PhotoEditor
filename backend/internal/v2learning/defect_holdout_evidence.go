package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// DefectHoldoutDecision must be read from the currently authorized, locked
// review row. Its fields alone do not grant source or publication authority.
type DefectHoldoutDecision struct {
	TenantID     uuid.UUID
	ReviewID     uuid.UUID
	RunID        uuid.UUID
	ActorID      uuid.UUID
	Decision     string
	Route        string
	DecisionHash string
	Revision     int64
	Selection    review.DefectSelection
}

// FrozenDefectHoldoutEvidence is a new immutable document, not an inferred
// artifact ID for the review row. Publication must register its exact bytes.
type FrozenDefectHoldoutEvidence struct {
	SchemaVersion  int                    `json:"schema_version"`
	TenantID       uuid.UUID              `json:"tenant_id"`
	EventID        uuid.UUID              `json:"event_id"`
	AssetVersionID uuid.UUID              `json:"asset_version_id"`
	ReviewID       uuid.UUID              `json:"review_id"`
	RunID          uuid.UUID              `json:"run_id"`
	ActorID        uuid.UUID              `json:"actor_id"`
	Decision       string                 `json:"decision"`
	Route          string                 `json:"route"`
	DecisionHash   string                 `json:"decision_hash"`
	Revision       int64                  `json:"revision"`
	Image          v2input.Ref            `json:"image"`
	Protection     v2input.Ref            `json:"protection"`
	Params         v2input.Ref            `json:"params"`
	Selection      review.DefectSelection `json:"selection"`
}

// CompileFrozenDefectHoldoutEvidence verifies the existing decision-hash
// formula and owns the selected IDs. A later SQL transaction must still prove
// current review, producer, consent and source authority before publication.
func CompileFrozenDefectHoldoutEvidence(decision DefectHoldoutDecision, eventID, assetVersionID uuid.UUID, image, protection, params v2input.Ref) (FrozenDefectHoldoutEvidence, []byte, string, error) {
	invalid := func() (FrozenDefectHoldoutEvidence, []byte, string, error) {
		return FrozenDefectHoldoutEvidence{}, nil, "", ErrInvalidTrainingDraft
	}
	if decision.TenantID == uuid.Nil || eventID == uuid.Nil || assetVersionID == uuid.Nil || decision.ReviewID == uuid.Nil || decision.RunID == uuid.Nil || decision.ActorID == uuid.Nil ||
		decision.Revision < 1 || decision.Revision > 9007199254740991 || decision.Selection.Revision != decision.Revision ||
		!validManifestRef(image) || !validManifestRef(protection) || !validManifestRef(params) ||
		image.ArtifactID == protection.ArtifactID || image.ArtifactID == params.ArtifactID || protection.ArtifactID == params.ArtifactID ||
		!validSHA(decision.DecisionHash) {
		return invalid()
	}
	selectionBody, err := canonical.Marshal(decision.Selection)
	if err != nil {
		return invalid()
	}
	var selection review.DefectSelection
	if json.Unmarshal(selectionBody, &selection) != nil || review.ValidateDefectSelectionStructure(selection) != nil ||
		selection.Image.ID != image.ArtifactID || selection.Image.SHA256 != image.SHA256 ||
		selection.Proposal.ID == image.ArtifactID || selection.LabelMap.ID == image.ArtifactID ||
		selection.Proposal.ID == selection.LabelMap.ID || selection.Proposal.ID == params.ArtifactID || selection.LabelMap.ID == params.ArtifactID ||
		selection.Proposal.ID == protection.ArtifactID || selection.LabelMap.ID == protection.ArtifactID {
		return invalid()
	}
	if decision.Decision != "accept" || decision.Route != "repair" || len(selection.Accepted) == 0 {
		return invalid()
	}
	computed, err := reviewedDefectDecisionHash(decision, selection)
	if err != nil || computed != decision.DecisionHash {
		return invalid()
	}
	frozen := FrozenDefectHoldoutEvidence{SchemaVersion: 1, TenantID: decision.TenantID, EventID: eventID, AssetVersionID: assetVersionID, ReviewID: decision.ReviewID,
		RunID: decision.RunID, ActorID: decision.ActorID, Decision: decision.Decision, Route: "repair",
		DecisionHash: computed, Revision: decision.Revision, Image: image, Protection: protection,
		Params: params, Selection: selection}
	body, err := canonical.Marshal(frozen)
	if err != nil {
		return invalid()
	}
	digest := sha256.Sum256(body)
	return frozen, body, hex.EncodeToString(digest[:]), nil
}

func reviewedDefectDecisionHash(decision DefectHoldoutDecision, selection review.DefectSelection) (string, error) {
	return canonical.Hash(struct {
		Version   string                 `json:"version"`
		TenantID  uuid.UUID              `json:"tenant_id"`
		ReviewID  uuid.UUID              `json:"review_id"`
		RunID     uuid.UUID              `json:"run_id"`
		Actor     uuid.UUID              `json:"actor_id"`
		Decision  string                 `json:"decision"`
		Selection review.DefectSelection `json:"selection"`
	}{"defect_decision_v1", decision.TenantID, decision.ReviewID, decision.RunID, decision.ActorID, decision.Decision, selection})
}
