package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestBackgroundCleanupPreviewApplyLockCancelledBeforeTransaction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := LockApplyBackgroundCleanupPreviewSelections(ctx, nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), PreparedReview{}, nil, ApplyDecision{})
	if err != context.Canceled || result != nil {
		t.Fatal("cancelled selection escaped", err)
	}
}

func TestBackgroundCleanupPreviewApplyLockRequiresTransaction(t *testing.T) {
	result, err := LockApplyBackgroundCleanupPreviewSelections(context.Background(), nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), PreparedReview{}, nil, ApplyDecision{})
	if err != ErrInvalid || result != nil {
		t.Fatal("selection accepted without transaction", err)
	}
}
