package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type dustHoldoutRepairParams struct {
	Image    v2input.Ref           `json:"image"`
	Proposal *v2input.Ref          `json:"proposal"`
	Review   *review.DustSelection `json:"review"`
}

// ValidateFrozenDustHoldoutMaterial checks exact SHA-pinned Params and proposal
// bytes against the reviewed decision. The caller must independently obtain
// those bytes from the SQL-authorized input closure.
func ValidateFrozenDustHoldoutMaterial(ctx context.Context,
	evidence FrozenDustHoldoutEvidence, paramsRaw, proposalRaw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(paramsRaw) < 2 || len(paramsRaw) > 1<<20 ||
		len(proposalRaw) < 2 || len(proposalRaw) > 1<<20 {
		return ErrInvalidTrainingDraft
	}
	paramsDigest := sha256.Sum256(paramsRaw)
	if hex.EncodeToString(paramsDigest[:]) != evidence.Params.SHA256 {
		return ErrInvalidTrainingDraft
	}
	canonicalParams, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(canonicalParams, paramsRaw) {
		return ErrInvalidTrainingDraft
	}
	var params dustHoldoutRepairParams
	decoder := json.NewDecoder(bytes.NewReader(paramsRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&params) != nil || params.Proposal == nil ||
		params.Review == nil || params.Image != evidence.StageImage ||
		*params.Proposal != evidence.Proposal ||
		!reflect.DeepEqual(*params.Review, evidence.Selection) {
		return ErrInvalidTrainingDraft
	}
	if !validDustHoldoutSelection(*params.Review, evidence.StageImage,
		evidence.Protection, evidence.Proposal, evidence.Params) {
		return ErrInvalidTrainingDraft
	}
	proposal := review.ArtifactRef{ID: evidence.Proposal.ArtifactID,
		SHA256: evidence.Proposal.SHA256}
	base := evidence.Selection.Base
	image := review.ArtifactRef{ID: evidence.StageImage.ArtifactID,
		SHA256: evidence.StageImage.SHA256}
	verified, err := review.ReadDustProposal(ctx, bytes.NewReader(proposalRaw), proposal, base)
	if err != nil {
		return ErrInvalidTrainingDraft
	}
	if _, err = review.ReadDustProposal(ctx, bytes.NewReader(proposalRaw), proposal, image); err != nil {
		return ErrInvalidTrainingDraft
	}
	snapshot, err := verified.Bind(review.DustSnapshot{TenantID: evidence.TenantID,
		ReviewID: evidence.ReviewID, RunID: evidence.RunID,
		Revision: evidence.Selection.Revision - 1, State: "pending",
		Proposal: proposal, Base: base})
	if err != nil {
		return ErrInvalidTrainingDraft
	}
	plan, err := review.PlanDust(snapshot, evidence.ActorID, review.Decision{
		ExpectedRevision: snapshot.Revision, CandidateHash: proposal.SHA256,
		Decision: "accept", Accepted: evidence.Selection.Accepted,
		Rejected: evidence.Selection.Rejected})
	if err != nil || plan.Route != "repair" ||
		plan.DecisionHash != evidence.DecisionHash ||
		!reflect.DeepEqual(plan.Selection, evidence.Selection) {
		return ErrInvalidTrainingDraft
	}
	return nil
}
