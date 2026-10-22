package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestDecisionProofPersistsExactShownSelection(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	previous, err := BuildDecisionProof(review, targets, request)
	if err != nil {
		t.Fatal(err)
	}
	selection := &PreviewSelection{Render: TargetDocument{ArtifactID: uuid.New(), SHA256: request.PlanSHA}, Evidence: TargetDocument{ArtifactID: uuid.New(), SHA256: request.PlanSHA}}
	request.TargetDecisions[0].Preview = selection
	proof, err := BuildDecisionProof(review, targets, request)
	if err != nil {
		t.Fatal(err)
	}
	var document decisionDocument
	if json.Unmarshal(proof.Body, &document) != nil || len(document.Targets) == 0 || document.Targets[0].Preview == nil || *document.Targets[0].Preview != *selection {
		t.Fatal("shown selection omitted from immutable decision")
	}
	if proof.SHA256 == previous.SHA256 || proof.RequestSHA256 == previous.RequestSHA256 {
		t.Fatal("selection did not bind immutable hashes")
	}
	selection.Render.ArtifactID = uuid.New()
	changed, err := BuildDecisionProof(review, targets, request)
	if err != nil || changed.SHA256 == proof.SHA256 {
		t.Fatal("changed shown output not bound")
	}
}
func TestShownBatchRequiresOneCopyForEachApproval(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	_, err := VerifyShownExposureBatch(uuid.New(), uuid.New(), uuid.New(), review, targets, request, nil)
	if err == nil {
		t.Fatal("approved target omitted")
	}
	for i := range request.TargetDecisions {
		request.TargetDecisions[i].Decision = "reject"
	}
	tasks, err := VerifyShownExposureBatch(uuid.New(), uuid.New(), uuid.New(), review, targets, request, nil)
	if err != nil || len(tasks) != 0 {
		t.Fatal("reject-only batch requires tasks", err)
	}
}

func TestShownBatchSQLProjectionRejectsMissingPublishedApproval(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	payload, err := PrepareShownBatchCommitInputs(uuid.New(), uuid.New(), uuid.New(), uuid.New(), review, targets, request, PublishedDecision{}, nil)
	if err == nil || payload.Tasks != nil || payload.Documents != nil {
		t.Fatal("incomplete approval projected")
	}
	for i := range request.TargetDecisions {
		request.TargetDecisions[i].Decision = "reject"
	}
	payload, err = PrepareShownBatchCommitInputs(uuid.New(), uuid.New(), uuid.New(), uuid.New(), review, targets, request, PublishedDecision{}, nil)
	if err == nil || payload.Tasks != nil {
		t.Fatal("reject-only request entered copy-child path")
	}
}
