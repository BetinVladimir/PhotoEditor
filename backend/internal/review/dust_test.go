package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func dustFixture(t *testing.T) ([]byte, ArtifactRef, ArtifactRef, string) {
	frames := make([]ArtifactRef, 5)
	hashes := make([]string, 5)
	for i := range frames {
		hashes[i] = strings.Repeat(string(rune('a'+i)), 64)
		frames[i] = ArtifactRef{uuid.New(), hashes[i]}
	}
	id, err := canonical.Hash(map[string]any{"algorithm": "sensor_dust_log_v1", "camera_id": "raw-" + strings.Repeat("f", 64), "sensor": []int64{420, 320}, "x": 12.5, "y": 18.5, "radius": 2.0, "frames": hashes})
	if err != nil {
		t.Fatal(err)
	}
	doc := dustDocument{Frames: frames, CameraID: "raw-" + strings.Repeat("f", 64), SensorWidth: 420, SensorHeight: 320, RawCrop: dustCrop{10, 10, 400, 300}, Algorithm: "sensor_dust_log_v1", Candidates: []dustCandidate{{id, 12.5, 18.5, 2, 5, 5, 1, "pending"}}, Warnings: []string{}}
	raw, _ := canonical.Marshal(doc)
	sum := sha256.Sum256(raw)
	return raw, ArtifactRef{uuid.New(), hex.EncodeToString(sum[:])}, frames[0], id
}

func TestDustProposalAndSubsetDecisionAreContentBound(t *testing.T) {
	raw, ref, base, id := dustFixture(t)
	proposal, err := ReadDustProposal(context.Background(), bytes.NewReader(raw), ref, base)
	if err != nil {
		t.Fatal(err)
	}
	s := DustSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, State: "pending", Proposal: ref, Base: base}
	s, err = proposal.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	p, err := PlanDust(s, uuid.New(), Decision{ExpectedRevision: 1, CandidateHash: ref.SHA256, Decision: "accept", Accepted: []string{id}})
	if err != nil || p.Route != "repair" || len(p.Selection.Accepted) != 1 || p.DecisionHash == "" {
		t.Fatal(p, err)
	}
	if _, err = PlanDust(s, uuid.New(), Decision{ExpectedRevision: 1, CandidateHash: ref.SHA256, Decision: "accept", Accepted: []string{strings.Repeat("0", 64)}}); err == nil {
		t.Fatal("unknown dust candidate accepted")
	}
}

func TestDustProposalRejectsWrongBaseAndMutation(t *testing.T) {
	raw, ref, _, _ := dustFixture(t)
	if _, err := ReadDustProposal(context.Background(), bytes.NewReader(raw), ref, ArtifactRef{uuid.New(), strings.Repeat("9", 64)}); err == nil {
		t.Fatal("foreign base accepted")
	}
	raw[len(raw)-2] ^= 1
	if _, err := ReadDustProposal(context.Background(), bytes.NewReader(raw), ref, ArtifactRef{}); err == nil {
		t.Fatal("mutated proposal accepted")
	}
}
