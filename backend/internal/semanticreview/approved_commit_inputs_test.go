package semanticreview

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestApprovedCommitOwnsRebuiltEvidence(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	tenant, actor, child := uuid.New(), uuid.New(), uuid.New()
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil {
		t.Fatal(err)
	}
	decision := PublishedDecision{BatchID: decisionDocumentOwner(tenant, prepared.Sources.RunID, actor, proof.RequestSHA256), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: proof.SHA256}, Proof: proof}
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := PrepareApprovedCommitInputs(tenant, actor, child, prepared, targets, request, decision, drafts)
	if err != nil || len(result.Nodes) != 1 {
		t.Fatal(err)
	}
	var rows []approvedParamsDocument
	if json.Unmarshal(result.Params, &rows) != nil || len(rows) != 1 || rows[0].TaskID != drafts[0].TaskID || rows[0].Body != string(drafts[0].Body) || result.Nodes[0].ParamsSHA != drafts[0].Params.SHA256 {
		t.Fatal("missing commit bindings")
	}
	original := append([]byte(nil), result.Decision.Body...)
	decision.Proof.Body[0] = '!'
	drafts[0].Body[0] = '!'
	if !bytes.Equal(result.Decision.Body, original) || rows[0].Body[0] != '{' {
		t.Fatal("published aliases changed owned commit")
	}
}

func TestApprovedCommitRejectsPublicationSubstitutions(t *testing.T) {
	for _, mutation := range []string{"batch", "decision_ref", "decision_body", "decision_sha", "request_sha", "task", "target", "params_id", "params_sha", "params_body", "kind", "missing", "duplicate", "child"} {
		t.Run(mutation, func(t *testing.T) {
			prepared, targets, request := decisionProofFixture(t)
			tenant, actor, child := uuid.New(), uuid.New(), uuid.New()
			proof, _ := BuildDecisionProof(prepared, targets, request)
			decision := PublishedDecision{BatchID: decisionDocumentOwner(tenant, prepared.Sources.RunID, actor, proof.RequestSHA256), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: proof.SHA256}, Proof: proof}
			drafts, _ := CompileApprovedTaskDrafts(child, prepared, targets, request)
			switch mutation {
			case "batch":
				decision.BatchID = uuid.New()
			case "decision_ref":
				decision.Ref.SHA256 = "bad"
			case "decision_body":
				decision.Proof.Body = []byte("{}")
			case "decision_sha":
				decision.Proof.SHA256 = "bad"
			case "request_sha":
				decision.Proof.RequestSHA256 = "bad"
			case "task":
				drafts[0].TaskID = uuid.New()
			case "target":
				drafts[0].TargetID = uuid.New()
			case "params_id":
				drafts[0].Params.ArtifactID = uuid.New()
			case "params_sha":
				drafts[0].Params.SHA256 = "bad"
			case "params_body":
				drafts[0].Body = []byte("{}")
			case "kind":
				drafts[0].Kind = "local_exposure"
			case "missing":
				drafts = nil
			case "duplicate":
				drafts = append(drafts, drafts[0])
			case "child":
				child = uuid.New()
			}
			result, err := PrepareApprovedCommitInputs(tenant, actor, child, prepared, targets, request, decision, drafts)
			if err == nil || len(result.Nodes) != 0 || len(result.Params) != 0 || len(result.Decision.Body) != 0 {
				t.Fatal("substitution produced commit input")
			}
		})
	}
}
