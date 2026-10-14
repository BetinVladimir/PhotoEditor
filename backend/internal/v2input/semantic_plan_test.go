package v2input

import (
	"bytes"
	"encoding/json"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestSemanticPlanPythonContractCompatibility(t *testing.T) {
	raw, err := os.ReadFile("testdata/semantic-plan-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Plan      json.RawMessage        `json:"plan"`
		Params    SemanticAssembleParams `json:"params"`
		Snapshot  json.RawMessage        `json:"snapshot"`
		Documents []struct {
			Ref  Ref             `json:"ref"`
			Body json.RawMessage `json:"body"`
		} `json:"documents"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture")
	}
	documents := make([]SemanticCandidateDocument, len(fixture.Documents))
	for n, document := range fixture.Documents {
		documents[n] = SemanticCandidateDocument{Ref: document.Ref, Body: document.Body}
	}
	plan, err := VerifySemanticPlan(semanticSnapshotBodyRef(fixture.Plan), fixture.Plan, fixture.Params, semanticSnapshotBodyRef(fixture.Snapshot), fixture.Snapshot, documents)
	if err != nil || len(plan.Candidates) != 3 || plan.Candidates[2].State != "rejected" {
		t.Fatal(plan, err)
	}
}

func semanticPlanFixture(t *testing.T, count int) (SemanticPlan, SemanticAssembleParams, Ref, []byte, []SemanticCandidateDocument) {
	t.Helper()
	params, _, _, snapshotRef, snapshotRaw, documents, _ := semanticAssemblyFixture(t, count)
	snapshot, err := DecodePinnedSessionInputSnapshot(snapshotRef, snapshotRaw)
	if err != nil {
		t.Fatal(err)
	}
	plan := SemanticPlan{SchemaVersion: 1, IntentSHA: params.Intent.SHA256, SnapshotDigest: snapshot.SnapshotDigest, ModelLockSHA: params.ModelLockSHA, Calibration: params.Calibration}
	for n := len(documents) - 1; n >= 0; n-- {
		candidate, err := DecodeSemanticCandidate(documents[n].Body)
		if err != nil {
			t.Fatal(err)
		}
		plan.Candidates = append(plan.Candidates, candidate)
	}
	return plan, params, snapshotRef, snapshotRaw, documents
}

func TestSemanticPlanFrozenCoverageAndOrder(t *testing.T) {
	for _, count := range []int{3, 5000} {
		plan, params, snapshotRef, snapshotRaw, documents := semanticPlanFixture(t, count)
		raw, _ := canonical.Marshal(plan)
		owned, err := VerifySemanticPlan(semanticSnapshotBodyRef(raw), raw, params, snapshotRef, snapshotRaw, documents)
		if err != nil || len(owned.Candidates) != count {
			t.Fatal(count, err)
		}
		// Decode owns nested pointers as well as the candidate slice.
		plan.Candidates[1].Mask.SHA256 = "changed"
		if owned.Candidates[1].Mask.SHA256 == "changed" {
			t.Fatal("alias")
		}
	}
}

func TestSemanticPlanRejectsSubstitutionsAndAmbiguousContracts(t *testing.T) {
	plan, params, snapshotRef, snapshotRaw, documents := semanticPlanFixture(t, 3)
	raw, _ := canonical.Marshal(plan)
	for _, mutation := range []func(*SemanticPlan){
		func(p *SemanticPlan) { p.Candidates[0], p.Candidates[1] = p.Candidates[1], p.Candidates[0] },
		func(p *SemanticPlan) { p.Candidates[1].Confidence = .7 },
		func(p *SemanticPlan) { p.Candidates = p.Candidates[:2] },
		func(p *SemanticPlan) { p.IntentSHA = p.ModelLockSHA },
		func(p *SemanticPlan) { p.SnapshotDigest = p.ModelLockSHA },
		func(p *SemanticPlan) { p.Calibration.SHA256 = p.ModelLockSHA },
	} {
		var changed SemanticPlan
		if json.Unmarshal(raw, &changed) != nil {
			t.Fatal("fixture")
		}
		mutation(&changed)
		body, _ := canonical.Marshal(changed)
		if _, err := VerifySemanticPlan(semanticSnapshotBodyRef(body), body, params, snapshotRef, snapshotRaw, documents); err == nil {
			t.Fatal("substitution accepted")
		}
	}
	if _, err := VerifySemanticPlan(semanticSnapshotBodyRef(raw), append(raw, ' '), params, snapshotRef, snapshotRaw, documents); err == nil {
		t.Fatal("changed bytes accepted")
	}
	for _, body := range [][]byte{
		bytes.Replace(raw, []byte(`"confidence":0`), []byte(`"confidence":null`), 1),
		bytes.Replace(raw, []byte(`"confidence":0,`), nil, 1),
		append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"schema_version":1}`)...),
		bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"unexpected":true`), 1),
	} {
		if _, err := DecodeSemanticPlan(body); err == nil {
			t.Fatal("ambiguous contract accepted")
		}
	}
}
