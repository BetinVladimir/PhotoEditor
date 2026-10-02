package review

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type ProtectionMask struct {
	Artifact       ArtifactRef     `json:"artifact"`
	Canvas         Canvas          `json:"canvas"`
	TransformChain json.RawMessage `json:"transform_chain"`
	ObjectID       *uuid.UUID      `json:"object_id"`
	Selector       string          `json:"selector"`
	Confidence     float64         `json:"confidence"`
	ModelLockID    *string         `json:"model_lock_id"`
	Protected      bool            `json:"protected"`
}

type RepairDocument struct {
	Params []byte
	Inputs []ArtifactRef
}

// BuildRepairDocument materializes the exact defects.repair params JSON and
// complete declared artifact set. Selection must be the immutable saved DB
// decision; this helper checks content binding, not actor authorization. Its
// reference must identify the canonical selection bytes already published.
func BuildRepairDocument(proposal VerifiedProposal, selection DefectSelection, selectionRef, resolved, resolvedImage ArtifactRef,
	protection ProtectionMask, alpha *ArtifactRef) (RepairDocument, error) {
	fail := func() (RepairDocument, error) { return RepairDocument{}, errors.New("invalid defect repair document") }
	if !proposal.ref.valid() || !selectionRef.valid() || !resolved.valid() || !resolvedImage.valid() || !protection.Artifact.valid() ||
		selection.Version != "defect_review_v1" || selection.Revision < 2 || selection.Revision > maxRevision ||
		selection.Proposal != proposal.ref || selection.Image != proposal.document.Image || selection.LabelMap != proposal.document.LabelMap ||
		len(selection.Accepted) == 0 || len(selection.Accepted) > 4096 || len(selection.Rejected) > 4096 || selection.Rejected == nil ||
		!protection.Protected || protection.Canvas != proposal.document.Canvas || !spaceIDPattern.MatchString(protection.Selector) ||
		protection.Confidence < 0 || protection.Confidence > 1 {
		return fail()
	}
	if protection.ObjectID != nil && *protection.ObjectID == uuid.Nil {
		return fail()
	}
	if protection.ModelLockID != nil {
		if _, err := domain.ParseDigest(*protection.ModelLockID); err != nil {
			return fail()
		}
	}
	a, err := canonical.Bytes(protection.TransformChain)
	if err != nil {
		return fail()
	}
	b, err := canonical.Bytes(proposal.document.TransformChain)
	if err != nil || !bytes.Equal(a, b) {
		return fail()
	}
	known := map[string]bool{}
	for _, c := range proposal.document.Candidates {
		known[c.ID] = true
	}
	seen := map[string]bool{}
	for _, list := range [][]string{selection.Accepted, selection.Rejected} {
		for _, id := range list {
			if !known[id] || seen[id] {
				return fail()
			}
			seen[id] = true
		}
	}
	raw, err := canonical.Marshal(selection)
	if err != nil {
		return fail()
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != selectionRef.SHA256 {
		return fail()
	}
	params, err := canonical.Marshal(struct {
		Image     ArtifactRef    `json:"image"`
		Proposal  ArtifactRef    `json:"proposal"`
		Review    ArtifactRef    `json:"review"`
		Resolved  ArtifactRef    `json:"resolved"`
		Protected ProtectionMask `json:"protected"`
	}{selection.Image, proposal.ref, selectionRef, resolved, protection})
	if err != nil || len(params) > 1048576 {
		return fail()
	}
	refs := []ArtifactRef{selection.Image, proposal.ref, selectionRef, resolved, resolvedImage, protection.Artifact, proposal.document.SourcePixels, selection.LabelMap}
	if alpha != nil {
		if !alpha.valid() {
			return fail()
		}
		refs = append(refs, *alpha)
	}
	byID := map[uuid.UUID]ArtifactRef{}
	for _, r := range refs {
		if old, ok := byID[r.ID]; ok && old != r {
			return fail()
		}
		byID[r.ID] = r
	}
	inputs := make([]ArtifactRef, 0, len(byID))
	for _, r := range byID {
		inputs = append(inputs, r)
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ID.String() < inputs[j].ID.String() })
	return RepairDocument{Params: params, Inputs: inputs}, nil
}
