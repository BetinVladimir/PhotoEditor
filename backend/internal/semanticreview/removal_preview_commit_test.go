package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRemovalCommitRejectsUnscopedOrCancelledCall(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if task, err := CommitRemovalPreviewRun(context.Background(), nil, who, uuid.New(), removalParamsFixture(), PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err == nil || task != uuid.Nil {
		t.Fatal("unscoped commit accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if task, err := CommitRemovalPreviewRun(ctx, nil, who, uuid.New(), removalParamsFixture(), PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err != context.Canceled || task != uuid.Nil {
		t.Fatal("lost cancellation", err)
	}
}
