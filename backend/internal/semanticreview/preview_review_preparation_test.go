package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPreviewReviewPreparationRejectsMissingAuthority(t *testing.T) {
	read := func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unauthorized object IO"); return nil, nil }
	got, err := PrepareScopedPreviewReview(context.Background(), nil, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{}, read, 1048576, "", "")
	if err == nil || got.Workflow != uuid.Nil {
		t.Fatal("unauthorized proposal returned")
	}
}
