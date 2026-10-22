package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestShownBackgroundCleanupBatchRejectsMissingApprovedCopy(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	result, err := VerifyShownBackgroundCleanupBatch(uuid.New(), uuid.New(), uuid.New(), review, targets, request, nil)
	if err == nil || result != nil {
		t.Fatal("approved copy omitted")
	}
}

func TestShownBackgroundCleanupBatchRejectsUnpublishedDecision(t *testing.T) {
	review, targets, request := decisionProofFixture(t)
	result, err := PrepareShownBackgroundCleanupBatchCommitInputs(uuid.New(), uuid.New(), uuid.New(), uuid.New(), review, targets, request, PublishedDecision{}, nil)
	if err == nil || result.Tasks != nil || result.Documents != nil {
		t.Fatal("unpublished decision admitted")
	}
}
