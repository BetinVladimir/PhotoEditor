package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestShownRemovalBatchRejectsMissingApprovedCopy(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	result, err := VerifyShownRemovalBatch(uuid.New(), uuid.New(), uuid.New(), review, targets, request, nil)
	if err == nil || result != nil {
		t.Fatal("approved copy omitted")
	}
}

func TestShownRemovalBatchRejectsUnpublishedDecision(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	result, err := PrepareShownRemovalBatchCommitInputs(uuid.New(), uuid.New(), uuid.New(), uuid.New(), review, targets, request, PublishedDecision{}, nil)
	if err == nil || result.Tasks != nil || result.Documents != nil {
		t.Fatal("unpublished decision admitted")
	}
}
