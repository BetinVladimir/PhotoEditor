package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestDustRepairParamsProjectInlineReviewProposal(t *testing.T) {
	ref := func(s string) v2input.Ref {
		sum := sha256.Sum256([]byte(s))
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	image, proposal := ref("image"), ref("proposal")
	selection := review.DustSelection{Version: "sensor_dust_review_v1", Proposal: review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256}, Base: review.ArtifactRef{ID: image.ArtifactID, SHA256: image.SHA256}, Revision: 2, Accepted: []string{}, Rejected: []string{}}
	raw, err := canonical.Marshal(map[string]any{"image": image, "proposal": proposal, "review": selection})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	children, err := DocumentChildren(raw, "Params", parent)
	if err != nil || len(children) != 2 || children[0].Contract != "WorkingImage" || children[0].Ref != image || children[1].Contract != "DustCandidates" || children[1].Ref != proposal {
		t.Fatal("inline dust review dependency projection failed", err, children)
	}
	selection.Base = review.ArtifactRef{ID: uuid.New(), SHA256: image.SHA256}
	bad, _ := canonical.Marshal(map[string]any{"image": image, "proposal": proposal, "review": selection})
	badSum := sha256.Sum256(bad)
	if _, err := DocumentChildren(bad, "Params", v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(badSum[:])}); err == nil {
		t.Fatal("foreign review base accepted")
	}
}
