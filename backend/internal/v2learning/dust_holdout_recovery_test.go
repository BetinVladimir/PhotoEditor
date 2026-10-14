package v2learning

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestReviewedDustRecoveryRetainsExactPublishedBytes(t *testing.T) {
	ref := func(char string) review.ArtifactRef {
		return review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(char, 64)}
	}
	stage := ref("a")
	proposal := ref("b")
	snapshot := review.DustSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(),
		RunID: uuid.New(), Revision: 1, State: "pending", Proposal: proposal,
		Base: stage, CandidateIDs: []string{strings.Repeat("c", 64)}}
	actor := uuid.New()
	plan, err := review.PlanDust(snapshot, actor, review.Decision{
		ExpectedRevision: 1, CandidateHash: proposal.SHA256,
		Decision: "accept", Accepted: snapshot.CandidateIDs})
	if err != nil {
		t.Fatal(err)
	}
	eventID, versionID, trainingRun := uuid.New(), uuid.New(), uuid.New()
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	stageImage := v2input.Ref{ArtifactID: stage.ID, SHA256: stage.SHA256}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
	proposalRef := v2input.Ref{ArtifactID: proposal.ID, SHA256: proposal.SHA256}
	document, body, sha, err := CompileFrozenDustHoldoutEvidence(DustHoldoutDecision{
		TenantID: snapshot.TenantID, ReviewID: snapshot.ReviewID,
		RunID: snapshot.RunID, ActorID: actor, Decision: "accept",
		Route: plan.Route, DecisionHash: plan.DecisionHash,
		Revision: plan.Revision, Selection: plan.Selection},
		eventID, versionID, image, stageImage, protection, proposalRef, params)
	if err != nil {
		t.Fatal(err)
	}
	manifestRef, err := ReviewedDustHoldoutRef(trainingRun, eventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	draft := TrainingDraftPrepared{TenantID: snapshot.TenantID, RunID: trainingRun}
	draft.Manifest.Examples = []ManifestExample{{EventID: eventID,
		RenderCase: TrainingRenderCase{DustEvidence: &manifestRef,
			Binding:      v2input.SessionVersionBinding{AssetVersionID: versionID, Source: image},
			WorkingImage: image, Protected: protection}}}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: snapshot.TenantID,
		RunID: trainingRun, WorkflowID: uuid.New(), Candidate: v2input.Ref{
			ArtifactID: uuid.New(), SHA256: strings.Repeat("1", 64)}}
	if err := ValidateFrozenDustHoldoutCase(lease, eventID,
		draft.Manifest.Examples[0].RenderCase, document); err != nil {
		t.Fatal("valid dust Evaluate case refused", err)
	}
	proof := FrozenEvaluateDustInputs{Evidence: document, Entries: [4]v2input.Entry{
		{Ref: manifestRef, Contract: "FrozenDustHoldoutEvidence"},
		{Ref: stageImage, Contract: "WorkingImage"},
		{Ref: proposalRef, Contract: "DustCandidates"},
		{Ref: params, Contract: "Params"},
	}}
	if err := validateFrozenDustEvaluateInputs(lease, eventID,
		draft.Manifest.Examples[0].RenderCase, proof); err != nil {
		t.Fatal("valid dust Evaluate closure refused", err)
	}
	proof.Entries[2].Ref = image
	if err := validateFrozenDustEvaluateInputs(lease, eventID,
		draft.Manifest.Examples[0].RenderCase, proof); err == nil {
		t.Fatal("foreign dust proposal accepted")
	}
	foreign := draft.Manifest.Examples[0].RenderCase
	foreign.WorkingImage = stageImage
	if err := ValidateFrozenDustHoldoutCase(lease, eventID,
		foreign, document); err == nil {
		t.Fatal("foreign source accepted for dust Evaluate")
	}
	draft.RecoveryBody, err = canonical.Marshal(trainingDraftRecovery{
		SchemaVersion: 1, ManifestSHA: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedReviewedDustHoldout{Document: document, Body: body, SHA256: sha}
	draft, err = AttachReviewedDustRecovery(draft,
		map[uuid.UUID]PreparedReviewedDustHoldout{eventID: prepared})
	if err != nil {
		t.Fatal(err)
	}
	var recovery trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil {
		t.Fatal("invalid recovery JSON")
	}
	read, err := reviewedDustFromRecovery(draft, []byte(recovery.DustJCS))
	if err != nil || read[eventID].SHA256 != sha ||
		string(read[eventID].Body) != string(body) {
		t.Fatal("dust recovery changed published bytes", err)
	}
	draft.Manifest.Examples[0].RenderCase.DustEvidence = &v2input.Ref{
		ArtifactID: uuid.New(), SHA256: sha}
	if _, err := reviewedDustFromRecovery(draft,
		[]byte(recovery.DustJCS)); err == nil {
		t.Fatal("accepted foreign manifest ref")
	}
}
