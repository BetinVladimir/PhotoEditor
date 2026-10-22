package v2learning

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenDefectHoldoutBindsReviewedDecisionAndOwnsSelection(t *testing.T) {
	ref := func(char string) review.ArtifactRef {
		return review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(char, 64)}
	}
	snapshot := review.DefectSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1,
		State: "pending", Proposal: ref("a"), Image: ref("b"), LabelMap: ref("c"), CandidateIDs: []string{strings.Repeat("d", 64)}}
	actor := uuid.New()
	plan, err := review.PlanDefects(snapshot, actor, review.Decision{ExpectedRevision: 1, CandidateHash: snapshot.Proposal.SHA256,
		Decision: "accept", Accepted: []string{snapshot.CandidateIDs[0]}})
	if err != nil {
		t.Fatal(err)
	}
	decision := DefectHoldoutDecision{TenantID: snapshot.TenantID, ReviewID: snapshot.ReviewID, RunID: snapshot.RunID,
		ActorID: actor, Decision: "accept", Route: plan.Route, DecisionHash: plan.DecisionHash,
		Revision: plan.Revision, Selection: plan.Selection}
	original := decision
	original.Selection.Accepted = append([]string{}, decision.Selection.Accepted...)
	original.Selection.Rejected = append([]string{}, decision.Selection.Rejected...)
	image := v2input.Ref{ArtifactID: snapshot.Image.ID, SHA256: snapshot.Image.SHA256}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
	eventID, assetVersionID := uuid.New(), uuid.New()
	frozen, body, sha, err := CompileFrozenDefectHoldoutEvidence(decision, eventID, assetVersionID, image, protection, params)
	if err != nil || frozen.DecisionHash != plan.DecisionHash || frozen.Route != "repair" || len(body) == 0 || !validSHA(sha) {
		t.Fatal("valid decision not frozen", err)
	}
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	expected := DefectHoldoutExpected{TenantID: snapshot.TenantID, EventID: eventID, AssetVersionID: assetVersionID, ReviewID: snapshot.ReviewID,
		RunID: snapshot.RunID, ActorID: actor, DecisionHash: plan.DecisionHash,
		Image: image, Protection: protection, Params: params}
	decoded, err := DecodeFrozenDefectHoldoutEvidence(body, artifact, expected)
	if err != nil || decoded.DecisionHash != frozen.DecisionHash || decoded.Selection.Accepted[0] != snapshot.CandidateIDs[0] {
		t.Fatal("registered frozen evidence not decoded", err)
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: snapshot.TenantID,
		RunID: uuid.New(), WorkflowID: uuid.New(), Candidate: image}
	caseRef := TrainingRenderCase{DefectEvidence: &artifact, WorkingImage: image,
		Protected: protection, Binding: v2input.SessionVersionBinding{AssetVersionID: assetVersionID}}
	if err := ValidateFrozenDefectHoldoutCase(lease, eventID, caseRef, decoded); err != nil {
		t.Fatal("valid holdout binding rejected", err)
	}
	proof := FrozenEvaluateDefectInputs{Evidence: decoded, Entries: [6]v2input.Entry{
		{Ref: artifact, Contract: "FrozenDefectHoldoutEvidence"},
		{Ref: params, Contract: "Params"},
		{Ref: v2input.Ref{ArtifactID: decoded.Selection.Proposal.ID,
			SHA256: decoded.Selection.Proposal.SHA256}, Contract: "DefectCandidates"},
		{Ref: v2input.Ref{ArtifactID: decoded.Selection.LabelMap.ID,
			SHA256: decoded.Selection.LabelMap.SHA256}, Contract: "DefectLabelMap"},
		{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("1", 64)}, Contract: "DefectReview"},
		{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("2", 64)}, Contract: "ResolvedStyle"},
	}}
	if err := validateFrozenDefectEvaluateInputs(lease, eventID, caseRef, proof); err != nil {
		t.Fatal("valid reviewed closure rejected", err)
	}
	for _, changed := range []func(*FrozenEvaluateDefectInputs){
		func(p *FrozenEvaluateDefectInputs) { p.Entries[3].Contract = "NpyArray" },
		func(p *FrozenEvaluateDefectInputs) { p.Entries[2].Ref.ArtifactID = uuid.New() },
		func(p *FrozenEvaluateDefectInputs) { p.Entries[4].Ref = p.Entries[1].Ref },
	} {
		altered := proof
		changed(&altered)
		if validateFrozenDefectEvaluateInputs(lease, eventID, caseRef, altered) == nil {
			t.Fatal("substituted reviewed closure accepted")
		}
	}
	for _, changed := range []func(*LearningEvaluateLease, *TrainingRenderCase, *FrozenDefectHoldoutEvidence){
		func(l *LearningEvaluateLease, _ *TrainingRenderCase, _ *FrozenDefectHoldoutEvidence) {
			l.TenantID = uuid.New()
		},
		func(_ *LearningEvaluateLease, c *TrainingRenderCase, _ *FrozenDefectHoldoutEvidence) {
			c.Binding.AssetVersionID = uuid.New()
		},
		func(_ *LearningEvaluateLease, c *TrainingRenderCase, _ *FrozenDefectHoldoutEvidence) {
			c.Protected.ArtifactID = uuid.New()
		},
		func(_ *LearningEvaluateLease, _ *TrainingRenderCase, d *FrozenDefectHoldoutEvidence) {
			d.EventID = uuid.New()
		},
	} {
		l, c, d := lease, caseRef, decoded
		changed(&l, &c, &d)
		if ValidateFrozenDefectHoldoutCase(l, eventID, c, d) == nil {
			t.Fatal("substituted holdout binding accepted")
		}
	}
	for _, altered := range []struct {
		body     []byte
		ref      v2input.Ref
		expected DefectHoldoutExpected
	}{
		{append(append([]byte(nil), body...), ' '), artifact, expected},
		{body, v2input.Ref{ArtifactID: artifact.ArtifactID, SHA256: strings.Repeat("0", 64)}, expected},
		{body, artifact, func() DefectHoldoutExpected { e := expected; e.ReviewID = uuid.New(); return e }()},
		{body, artifact, func() DefectHoldoutExpected { e := expected; e.EventID = uuid.New(); return e }()},
		{body, artifact, func() DefectHoldoutExpected { e := expected; e.AssetVersionID = uuid.New(); return e }()},
		{body, artifact, func() DefectHoldoutExpected { e := expected; e.Image.ArtifactID = uuid.New(); return e }()},
	} {
		if _, err := DecodeFrozenDefectHoldoutEvidence(altered.body, altered.ref, altered.expected); err == nil {
			t.Fatal("substituted frozen evidence decoded")
		}
	}
	decision.Selection.Accepted[0] = strings.Repeat("0", 64)
	if frozen.Selection.Accepted[0] != snapshot.CandidateIDs[0] || bytes.Contains(body, []byte(strings.Repeat("0", 64))) {
		t.Fatal("frozen evidence borrowed caller selection")
	}
	for _, change := range []func(*DefectHoldoutDecision, *v2input.Ref, *v2input.Ref, *v2input.Ref){
		func(d *DefectHoldoutDecision, _, _, _ *v2input.Ref) { d.DecisionHash = strings.Repeat("0", 64) },
		func(d *DefectHoldoutDecision, _, _, _ *v2input.Ref) { d.Route = "base" },
		func(d *DefectHoldoutDecision, _, _, _ *v2input.Ref) { d.Revision++ },
		func(d *DefectHoldoutDecision, _, _, _ *v2input.Ref) {
			d.Selection.Accepted = []string{strings.Repeat("0", 64)}
		},
		func(_ *DefectHoldoutDecision, i, _, _ *v2input.Ref) { i.ArtifactID = uuid.New() },
		func(_ *DefectHoldoutDecision, i, p, _ *v2input.Ref) { *p = *i },
		func(d *DefectHoldoutDecision, _, p, _ *v2input.Ref) { p.ArtifactID = d.Selection.Proposal.ID },
		func(_ *DefectHoldoutDecision, _, _, p *v2input.Ref) { p.SHA256 = "invalid" },
	} {
		candidate, ci, cp, ca := original, image, protection, params
		change(&candidate, &ci, &cp, &ca)
		if _, _, _, err := CompileFrozenDefectHoldoutEvidence(candidate, eventID, assetVersionID, ci, cp, ca); err == nil {
			t.Fatal("substituted reviewed holdout accepted")
		}
	}
}

func TestFrozenDefectHoldoutRefusesUnappliedBaseAndSkipRoutes(t *testing.T) {
	ref := func(char string) review.ArtifactRef {
		return review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat(char, 64)}
	}
	snapshot := review.DefectSnapshot{TenantID: uuid.New(), ReviewID: uuid.New(), RunID: uuid.New(), Revision: 1,
		State: "pending", Proposal: ref("a"), Image: ref("b"), LabelMap: ref("c"), CandidateIDs: []string{strings.Repeat("d", 64)}}
	actor := uuid.New()
	image := v2input.Ref{ArtifactID: snapshot.Image.ID, SHA256: snapshot.Image.SHA256}
	protection := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
	for _, name := range []string{"reject", "skip"} {
		plan, err := review.PlanDefects(snapshot, actor, review.Decision{ExpectedRevision: 1,
			CandidateHash: snapshot.Proposal.SHA256, Decision: name})
		if err != nil {
			t.Fatal(err)
		}
		decision := DefectHoldoutDecision{TenantID: snapshot.TenantID, ReviewID: snapshot.ReviewID,
			RunID: snapshot.RunID, ActorID: actor, Decision: name, Route: plan.Route,
			DecisionHash: plan.DecisionHash, Revision: plan.Revision, Selection: plan.Selection}
		if _, _, _, err := CompileFrozenDefectHoldoutEvidence(decision, uuid.New(), uuid.New(), image, protection, params); err == nil {
			t.Fatal("unapplied review route frozen as a repair", name)
		}
	}
}
