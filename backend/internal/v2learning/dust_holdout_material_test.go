package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenDustHoldoutMaterialReplaysReviewedCandidate(t *testing.T) {
	pin := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("8", 64)}
	stageImage := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	frames := []review.ArtifactRef{{ID: stageImage.ArtifactID, SHA256: stageImage.SHA256}}
	hashes := []string{stageImage.SHA256}
	for i := 0; i < 4; i++ {
		hash := strings.Repeat(string(rune('b'+i)), 64)
		frames = append(frames, review.ArtifactRef{ID: uuid.New(), SHA256: hash})
		hashes = append(hashes, hash)
	}
	camera := "raw-" + strings.Repeat("f", 64)
	candidateID, err := canonical.Hash(map[string]any{"algorithm": "sensor_dust_log_v1",
		"camera_id": camera, "sensor": []int64{64, 64}, "x": 10.0,
		"y": 11.0, "radius": 2.0, "frames": hashes})
	if err != nil {
		t.Fatal(err)
	}
	proposalRaw, err := canonical.Marshal(map[string]any{
		"frames": frames, "camera_id": camera, "sensor_width": 64,
		"sensor_height": 64, "raw_crop": map[string]int{"x": 0, "y": 0,
			"width": 64, "height": 64}, "algorithm": "sensor_dust_log_v1",
		"warnings": []string{}, "candidates": []map[string]any{{
			"id": candidateID, "sensor_x": 10.0, "sensor_y": 11.0,
			"radius_sensor_px": 2.0, "observed_frames": 5,
			"eligible_frames": 5, "recurrence": 1.0, "decision": "pending"}}})
	if err != nil {
		t.Fatal(err)
	}
	proposal := pin(proposalRaw)
	verified, err := review.ReadDustProposal(context.Background(),
		bytes.NewReader(proposalRaw), review.ArtifactRef{ID: proposal.ArtifactID,
			SHA256: proposal.SHA256}, frames[0])
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := verified.Bind(review.DustSnapshot{TenantID: uuid.New(),
		ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1, State: "pending",
		Proposal: review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256},
		Base:     frames[0]})
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	plan, err := review.PlanDust(snapshot, actor, review.Decision{
		ExpectedRevision: 1, CandidateHash: proposal.SHA256,
		Decision: "accept", Accepted: []string{candidateID}})
	if err != nil {
		t.Fatal(err)
	}
	paramsRaw, err := canonical.Marshal(dustHoldoutRepairParams{
		Image: stageImage, Proposal: &proposal, Review: &plan.Selection})
	if err != nil {
		t.Fatal(err)
	}
	params := pin(paramsRaw)
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("9", 64)}
	evidence, _, _, err := CompileFrozenDustHoldoutEvidence(DustHoldoutDecision{
		TenantID: snapshot.TenantID, ReviewID: snapshot.ReviewID,
		RunID: snapshot.RunID, ActorID: actor, Decision: "accept",
		Route: plan.Route, DecisionHash: plan.DecisionHash,
		Revision: plan.Revision, Selection: plan.Selection},
		uuid.New(), uuid.New(), image, stageImage, protection, proposal, params)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFrozenDustHoldoutMaterial(context.Background(),
		evidence, paramsRaw, proposalRaw); err != nil {
		t.Fatal(err)
	}
	altered := append([]byte(nil), paramsRaw...)
	altered = append(altered, '\n')
	if ValidateFrozenDustHoldoutMaterial(context.Background(), evidence,
		altered, proposalRaw) == nil {
		t.Fatal("accepted changed Params bytes")
	}
	if ValidateFrozenDustHoldoutMaterial(context.Background(), evidence,
		paramsRaw, []byte(`{}`)) == nil {
		t.Fatal("accepted changed proposal")
	}
}
