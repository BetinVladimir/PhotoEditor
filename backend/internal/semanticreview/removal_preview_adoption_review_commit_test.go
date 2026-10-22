package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestRemovalReviewCommitRejectsMissingTransaction(t *testing.T) {
	response, err := CommitShownRemoval(context.Background(), nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "request", PreparedReview{}, nil, ApplyDecision{}, PublishedDecision{}, nil)
	if err != ErrInvalid || response.Status != 0 {
		t.Fatal("missing transaction accepted", err)
	}
}
