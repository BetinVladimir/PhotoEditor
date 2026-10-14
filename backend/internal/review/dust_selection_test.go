package review

import (
	"bytes"
	"context"
	"testing"

	"photoeditor/backend/internal/canonical"
)

func TestDustSelectionBindsExactProposalCandidate(t *testing.T) {
	raw, ref, base, id := dustFixture(t)
	proposal, err := ReadDustProposal(context.Background(), bytes.NewReader(raw), ref, base)
	if err != nil {
		t.Fatal(err)
	}
	selection := DustSelection{Version: "sensor_dust_review_v1", Proposal: ref, Base: base, Revision: 2, Accepted: []string{id}, Rejected: []string{}}
	wire, err := canonical.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDustSelection(wire)
	if err != nil || proposal.ValidateSelection(parsed) != nil {
		t.Fatal("valid selection was rejected", err)
	}
	selection.Accepted[0] = digestDustBytes([]byte("unrelated"))
	if err := proposal.ValidateSelection(selection); err == nil {
		t.Fatal("unknown candidate was accepted")
	}
	selection.Accepted = []string{id}
	selection.Rejected = []string{id}
	if err := ValidateDustSelectionStructure(selection); err == nil {
		t.Fatal("duplicate cross-decision ID was accepted")
	}
}
