package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
	"time"
)

func TestBackgroundCleanupSourcePreparationPreservesEarlyContextFailure(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		expected := context.Canceled
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
			expected = context.DeadlineExceeded
		} else {
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		reader := func(context.Context, PlanSource) ([]byte, error) {
			t.Fatal("object read after context failure")
			return nil, nil
		}
		result, transform, err := LoadBackgroundCleanupPreviewSourceDocuments(ctx, nil, auth.Identity{},
			uuid.Nil, uuid.Nil, ScopedPreviewReview{}, PreviewRequest{}, reader,
			ExposureOwnerCosts{}, ExposureResourceEstimate{}, BackgroundCleanupQualificationPin{}, 0, 128, 8)
		cancel()
		if err != expected || result.TargetID != uuid.Nil || transform != nil {
			t.Fatal("context failure not preserved", deadline, err)
		}
	}
}
