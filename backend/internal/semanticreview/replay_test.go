package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestApplyRequestHashSeparatesParentRuns(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := HashApplyRequest(prepared.Sources.RunID, request)
	if err != nil || expected != proof.RequestSHA256 {
		t.Fatal("proof/replay fingerprint mismatch", err)
	}
	other, err := HashApplyRequest(uuid.New(), request)
	if err != nil || other == expected {
		t.Fatal("different intent parent shares fingerprint", err)
	}
	if _, err := HashApplyRequest(uuid.Nil, request); err == nil {
		t.Fatal("missing parent accepted")
	}
	request.TargetDecisions[0].Decision = "invalid"
	if _, err := HashApplyRequest(prepared.Sources.RunID, request); err == nil {
		t.Fatal("invalid request fingerprint accepted")
	}
}

func TestDecisionProofCannotPromoteUnsupportedSeed(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	for n := range targets {
		if targets[n].State == "unsupported" {
			targets[n].State = "proposed"
			request.TargetDecisions = []TargetDecision{{TargetID: targets[n].ID, Decision: "approve"}}
			proof, err := BuildDecisionProof(prepared, targets, request)
			if err == nil || len(proof.Body) != 0 {
				t.Fatal("unsupported producer seed promoted")
			}
			return
		}
	}
	t.Fatal("unsupported fixture required")
}
