package v2learning

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenPortraitDetailCaseRequiresExactTrainingBinding(t *testing.T) {
	origin := detailEvidenceOrigin()
	origin.MaskArtifacts = []v2input.Ref{detailEvidenceRef(12), detailEvidenceRef(13)}
	evidence, body, sha, err := CompileFrozenPortraitDetailHoldoutEvidence(
		origin, origin.MaskArtifacts)
	if err != nil {
		t.Fatal(err)
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: origin.TenantID,
		RunID: uuid.New(), WorkflowID: uuid.New(),
		Candidate: detailEvidenceRef(18)}
	ref, err := PortraitDetailHoldoutRef(lease.RunID, origin.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	caseRef := TrainingRenderCase{DetailEvidence: &ref,
		Binding: v2input.SessionVersionBinding{
			AssetVersionID: origin.AssetVersionID, Source: origin.Source},
		WorkingImage: origin.Source, Protected: origin.Protection}
	if err := ValidateFrozenPortraitDetailCase(lease, origin.EventID,
		caseRef, evidence); err != nil {
		t.Fatal("valid linked detail evidence refused", err)
	}
	changed := evidence
	changed.ResultID = uuid.New()
	if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(body, ref,
		detailHoldoutExpected(changed)); err == nil {
		t.Fatal("changed Apply producer accepted by exact decoder")
	}
	foreign := caseRef
	foreign.WorkingImage = detailEvidenceRef(19)
	if err := ValidateFrozenPortraitDetailCase(lease, origin.EventID,
		foreign, evidence); err == nil {
		t.Fatal("foreign training source accepted")
	}
	foreign = caseRef
	badRef := ref
	badRef.ArtifactID = uuid.New()
	foreign.DetailEvidence = &badRef
	if err := ValidateFrozenPortraitDetailCase(lease, origin.EventID,
		foreign, evidence); err == nil {
		t.Fatal("foreign evidence artifact accepted")
	}
}

func TestFrozenPortraitDetailEvaluateInputsRejectSubstitution(t *testing.T) {
	origin := detailEvidenceOrigin()
	origin.MaskArtifacts = []v2input.Ref{detailEvidenceRef(12), detailEvidenceRef(13)}
	evidence, _, sha, err := CompileFrozenPortraitDetailHoldoutEvidence(
		origin, origin.MaskArtifacts)
	if err != nil {
		t.Fatal(err)
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: origin.TenantID,
		RunID: uuid.New(), WorkflowID: uuid.New(), Candidate: detailEvidenceRef(18)}
	ref, err := PortraitDetailHoldoutRef(lease.RunID, origin.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	caseRef := TrainingRenderCase{DetailEvidence: &ref,
		Binding: v2input.SessionVersionBinding{AssetVersionID: origin.AssetVersionID,
			Source: origin.Source}, WorkingImage: origin.Source,
		Protected: origin.Protection}
	proof := FrozenEvaluateDetailInputs{Evidence: evidence,
		Entries: []v2input.Entry{
			{Ref: ref, Contract: "FrozenPortraitDetailHoldoutEvidence"},
			{Ref: evidence.Image, Contract: "WorkingImage"},
			{Ref: evidence.Params, Contract: "PortraitParams"},
			{Ref: evidence.Resolved, Contract: "ResolvedStyle"},
			{Ref: evidence.MaskArtifacts[0], Contract: "NpyArray"},
			{Ref: evidence.MaskArtifacts[1], Contract: "NpyArray"},
		}}
	if err := validateFrozenDetailEvaluateInputs(lease, origin.EventID,
		caseRef, proof); err != nil {
		t.Fatal(err)
	}
	changed := proof
	changed.Entries = append([]v2input.Entry(nil), proof.Entries...)
	changed.Entries[4].Ref = detailEvidenceRef(20)
	if err := validateFrozenDetailEvaluateInputs(lease, origin.EventID,
		caseRef, changed); err == nil {
		t.Fatal("substituted mask accepted")
	}
	changed.Entries[4] = changed.Entries[5]
	if err := validateFrozenDetailEvaluateInputs(lease, origin.EventID,
		caseRef, changed); err == nil {
		t.Fatal("duplicate mask accepted")
	}
}
