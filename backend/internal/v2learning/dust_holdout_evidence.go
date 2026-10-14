package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// DustHoldoutDecision must come from a locked, currently authorized review.
// This value alone does not grant publication or input access.
type DustHoldoutDecision struct {
	TenantID, ReviewID, RunID, ActorID uuid.UUID
	Decision, Route, DecisionHash      string
	Revision                           int64
	Selection                          review.DustSelection
}

type FrozenDustHoldoutEvidence struct {
	SchemaVersion  int                  `json:"schema_version"`
	TenantID       uuid.UUID            `json:"tenant_id"`
	EventID        uuid.UUID            `json:"event_id"`
	AssetVersionID uuid.UUID            `json:"asset_version_id"`
	ReviewID       uuid.UUID            `json:"review_id"`
	RunID          uuid.UUID            `json:"run_id"`
	ActorID        uuid.UUID            `json:"actor_id"`
	Decision       string               `json:"decision"`
	Route          string               `json:"route"`
	DecisionHash   string               `json:"decision_hash"`
	Image          v2input.Ref          `json:"image"`
	StageImage     v2input.Ref          `json:"stage_image"`
	Protection     v2input.Ref          `json:"protection"`
	Proposal       v2input.Ref          `json:"proposal"`
	Params         v2input.Ref          `json:"params"`
	Selection      review.DustSelection `json:"selection"`
}

func validDustHoldoutSelection(selection review.DustSelection, stageImage, protection, proposal, params v2input.Ref) bool {
	if selection.Version != "sensor_dust_review_v1" || selection.Revision < 1 || selection.Revision > 9007199254740991 ||
		selection.Proposal.ID != proposal.ArtifactID || selection.Proposal.SHA256 != proposal.SHA256 ||
		selection.Base.ID == uuid.Nil || !validSHA(selection.Base.SHA256) ||
		selection.Base.ID == protection.ArtifactID ||
		selection.Base.ID == proposal.ArtifactID ||
		selection.Base.ID == params.ArtifactID || len(selection.Accepted) < 1 ||
		len(selection.Accepted) > 4096 || len(selection.Rejected) > 4096 {
		return false
	}
	if selection.Base.ID == stageImage.ArtifactID && selection.Base.SHA256 != stageImage.SHA256 {
		return false
	}
	if !sort.StringsAreSorted(selection.Accepted) || !sort.StringsAreSorted(selection.Rejected) {
		return false
	}
	seen := make(map[string]bool, len(selection.Accepted)+len(selection.Rejected))
	for _, set := range [][]string{selection.Accepted, selection.Rejected} {
		for _, id := range set {
			if !validSHA(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
	}
	return true
}

// CompileFrozenDustHoldoutEvidence owns the decision bytes and checks the
// existing Go review hash. SQL must still prove live review/source authority.
func CompileFrozenDustHoldoutEvidence(decision DustHoldoutDecision, eventID, assetVersionID uuid.UUID,
	image, stageImage, protection, proposal, params v2input.Ref) (FrozenDustHoldoutEvidence, []byte, string, error) {
	invalid := func() (FrozenDustHoldoutEvidence, []byte, string, error) {
		return FrozenDustHoldoutEvidence{}, nil, "", ErrInvalidTrainingDraft
	}
	if decision.TenantID == uuid.Nil || decision.ReviewID == uuid.Nil || decision.RunID == uuid.Nil || decision.ActorID == uuid.Nil ||
		eventID == uuid.Nil || assetVersionID == uuid.Nil || decision.Decision != "accept" || decision.Route != "repair" ||
		decision.Revision < 1 || decision.Revision > 9007199254740991 || decision.Selection.Revision != decision.Revision ||
		!validSHA(decision.DecisionHash) || !validManifestRef(image) ||
		!validManifestRef(stageImage) || !validManifestRef(protection) ||
		!validManifestRef(proposal) || !validManifestRef(params) ||
		image.ArtifactID == stageImage.ArtifactID ||
		image.ArtifactID == protection.ArtifactID || image.ArtifactID == proposal.ArtifactID || image.ArtifactID == params.ArtifactID ||
		stageImage.ArtifactID == protection.ArtifactID || stageImage.ArtifactID == proposal.ArtifactID || stageImage.ArtifactID == params.ArtifactID ||
		protection.ArtifactID == proposal.ArtifactID || protection.ArtifactID == params.ArtifactID || proposal.ArtifactID == params.ArtifactID {
		return invalid()
	}
	selectionRaw, err := canonical.Marshal(decision.Selection)
	if err != nil {
		return invalid()
	}
	var selection review.DustSelection
	if json.Unmarshal(selectionRaw, &selection) != nil ||
		!validDustHoldoutSelection(selection, stageImage, protection, proposal, params) {
		return invalid()
	}
	hash, err := canonical.Hash(struct {
		Version   string               `json:"version"`
		TenantID  uuid.UUID            `json:"tenant_id"`
		ReviewID  uuid.UUID            `json:"review_id"`
		RunID     uuid.UUID            `json:"run_id"`
		ActorID   uuid.UUID            `json:"actor_id"`
		Decision  string               `json:"decision"`
		Selection review.DustSelection `json:"selection"`
	}{"sensor_dust_decision_v1", decision.TenantID, decision.ReviewID, decision.RunID, decision.ActorID, "accept", selection})
	if err != nil || hash != decision.DecisionHash {
		return invalid()
	}
	frozen := FrozenDustHoldoutEvidence{1, decision.TenantID, eventID, assetVersionID,
		decision.ReviewID, decision.RunID, decision.ActorID, "accept", "repair", hash,
		image, stageImage, protection, proposal, params, selection}
	body, err := canonical.Marshal(frozen)
	if err != nil || len(body) > 1048576 {
		return invalid()
	}
	digest := sha256.Sum256(body)
	return frozen, body, hex.EncodeToString(digest[:]), nil
}
