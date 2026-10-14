package v2learning

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// DustHoldoutExpected comes from a fresh SQL review/source authority read,
// never from the stored evidence document or caller-authored request.
type DustHoldoutExpected struct {
	TenantID, EventID, AssetVersionID, ReviewID, RunID, ActorID uuid.UUID
	DecisionHash                                                string
	Image, StageImage, Protection, Proposal, Params             v2input.Ref
}

// DecodeFrozenDustHoldoutEvidence checks exact published bytes against a
// separately authorized SQL identity. It does not grant source access.
func DecodeFrozenDustHoldoutEvidence(raw []byte, ref v2input.Ref,
	expected DustHoldoutExpected) (FrozenDustHoldoutEvidence, error) {
	invalid := func() (FrozenDustHoldoutEvidence, error) { return FrozenDustHoldoutEvidence{}, ErrInvalidTrainingDraft }
	if len(raw) < 2 || len(raw) > 1<<20 || !validManifestRef(ref) ||
		expected.TenantID == uuid.Nil || expected.EventID == uuid.Nil || expected.AssetVersionID == uuid.Nil ||
		expected.ReviewID == uuid.Nil || expected.RunID == uuid.Nil || expected.ActorID == uuid.Nil ||
		!validSHA(expected.DecisionHash) || !validManifestRef(expected.Image) ||
		!validManifestRef(expected.StageImage) ||
		!validManifestRef(expected.Protection) || !validManifestRef(expected.Proposal) ||
		!validManifestRef(expected.Params) {
		return invalid()
	}
	var parsed FrozenDustHoldoutEvidence
	if json.Unmarshal(raw, &parsed) != nil || parsed.SchemaVersion != 1 ||
		parsed.TenantID != expected.TenantID || parsed.EventID != expected.EventID ||
		parsed.AssetVersionID != expected.AssetVersionID || parsed.ReviewID != expected.ReviewID ||
		parsed.RunID != expected.RunID || parsed.ActorID != expected.ActorID ||
		parsed.DecisionHash != expected.DecisionHash || parsed.Image != expected.Image ||
		parsed.StageImage != expected.StageImage ||
		parsed.Protection != expected.Protection || parsed.Proposal != expected.Proposal ||
		parsed.Params != expected.Params {
		return invalid()
	}
	decision := DustHoldoutDecision{TenantID: parsed.TenantID, ReviewID: parsed.ReviewID,
		RunID: parsed.RunID, ActorID: parsed.ActorID, Decision: parsed.Decision,
		Route: parsed.Route, DecisionHash: parsed.DecisionHash,
		Revision: parsed.Selection.Revision, Selection: parsed.Selection}
	frozen, canonicalBody, sha, err := CompileFrozenDustHoldoutEvidence(decision,
		parsed.EventID, parsed.AssetVersionID, parsed.Image, parsed.StageImage, parsed.Protection,
		parsed.Proposal, parsed.Params)
	if err != nil || sha != ref.SHA256 || !bytes.Equal(canonicalBody, raw) {
		return invalid()
	}
	return frozen, nil
}
