package review

import (
	"bytes"
	"errors"
	"sort"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// ParseDustSelection validates a nested immutable review document. It does not
// establish actor consent, revision authority, or proposal provenance.
func ParseDustSelection(raw []byte) (DustSelection, error) {
	fail := func() (DustSelection, error) { return DustSelection{}, errors.New("invalid sensor dust selection") }
	if len(raw) < 2 || len(raw) > 1<<20 {
		return fail()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return fail()
	}
	var selection DustSelection
	if decodeExact(raw, &selection) != nil || ValidateDustSelectionStructure(selection) != nil {
		return fail()
	}
	return selection, nil
}

func ValidateDustSelectionStructure(selection DustSelection) error {
	invalid := errors.New("invalid sensor dust selection")
	if selection.Version != "sensor_dust_review_v1" || !selection.Proposal.valid() || !selection.Base.valid() || selection.Revision < 1 || selection.Revision > maxRevision || selection.Accepted == nil || selection.Rejected == nil || len(selection.Accepted) > 4096 || len(selection.Rejected) > 4096 {
		return invalid
	}
	seen := map[string]bool{}
	for _, list := range [][]string{selection.Accepted, selection.Rejected} {
		if !sort.StringsAreSorted(list) {
			return invalid
		}
		for _, id := range list {
			if _, err := domain.ParseDigest(id); err != nil || seen[id] {
				return invalid
			}
			seen[id] = true
		}
	}
	return nil
}

// ValidateSelection binds all accepted/rejected IDs to one verified proposal.
func (p VerifiedDustProposal) ValidateSelection(selection DustSelection) error {
	if err := ValidateDustSelectionStructure(selection); err != nil {
		return err
	}
	if selection.Proposal != p.ref {
		return errors.New("stale sensor dust proposal")
	}
	foundBase := false
	for _, frame := range p.document.Frames {
		foundBase = foundBase || frame == selection.Base
	}
	if !foundBase {
		return errors.New("foreign sensor dust base")
	}
	known := make(map[string]bool, len(p.document.Candidates))
	for _, candidate := range p.document.Candidates {
		known[candidate.ID] = true
	}
	for _, list := range [][]string{selection.Accepted, selection.Rejected} {
		for _, id := range list {
			if !known[id] {
				return errors.New("unknown sensor dust decision")
			}
		}
	}
	return nil
}
