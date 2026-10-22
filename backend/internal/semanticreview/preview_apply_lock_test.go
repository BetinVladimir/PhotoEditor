package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestPreviewApplyLockCancelledBeforeTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := LockApplyPreviewSelections(ctx, nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), PreparedReview{}, nil, ApplyDecision{})
	if err != context.Canceled || result != nil {
		t.Fatal("cancelled selection escaped", err)
	}
}
