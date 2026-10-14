package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"photoeditor/backend/internal/domain"
)

// ParseDefectSelection checks immutable wire structure, not saved-review ACL
// or current revision authority. Pending candidates remain omitted.
func ParseDefectSelection(raw []byte, ref ArtifactRef) (DefectSelection, error) {
	fail := func() (DefectSelection, error) { return DefectSelection{}, errors.New("invalid defect selection") }
	if !ref.valid() || len(raw) > 1048576 {
		return fail()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return fail()
	}
	var selection DefectSelection
	if decodeExact(raw, &selection) != nil || validateDefectSelectionStructure(selection) != nil {
		return fail()
	}
	return selection, nil
}

func validateDefectSelectionStructure(selection DefectSelection) error {
	invalid := errors.New("invalid defect selection")
	if selection.Version != "defect_review_v1" || !selection.Proposal.valid() || !selection.Image.valid() || !selection.LabelMap.valid() ||
		selection.Revision < 1 || selection.Revision > maxRevision || selection.Accepted == nil || selection.Rejected == nil ||
		len(selection.Accepted) > 4096 || len(selection.Rejected) > 4096 {
		return invalid
	}
	seen := map[string]bool{}
	for _, list := range [][]string{selection.Accepted, selection.Rejected} {
		for _, id := range list {
			if _, err := domain.ParseDigest(id); err != nil || seen[id] {
				return invalid
			}
			seen[id] = true
		}
	}
	return nil
}

// ValidateDefectSelectionStructure validates a typed selection without
// claiming that it came from an authorized or immutable review row.
func ValidateDefectSelectionStructure(selection DefectSelection) error {
	return validateDefectSelectionStructure(selection)
}

// ValidateSelection binds explicit decisions to this exact verified proposal.
// It does not approve a review or replace current transaction authorization.
func (p VerifiedProposal) ValidateSelection(selection DefectSelection) error {
	// Bound list lengths before allocating any canonical representation.
	if err := validateDefectSelectionStructure(selection); err != nil {
		return err
	}
	if !p.ref.valid() || selection.Proposal != p.ref || selection.Image != p.document.Image || selection.LabelMap != p.document.LabelMap {
		return errors.New("stale defect selection")
	}
	known := map[string]bool{}
	for _, candidate := range p.document.Candidates {
		known[candidate.ID] = true
	}
	for _, list := range [][]string{selection.Accepted, selection.Rejected} {
		for _, id := range list {
			if !known[id] {
				return errors.New("unknown defect decision")
			}
		}
	}
	return nil
}
