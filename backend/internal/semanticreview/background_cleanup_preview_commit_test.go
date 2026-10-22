package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupCommitRejectsUnscopedOrCancelledCall(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if task, err := CommitBackgroundCleanupPreviewRun(context.Background(), nil, who, uuid.New(), backgroundCleanupParamsFixture(), PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err == nil || task != uuid.Nil {
		t.Fatal("unscoped commit accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if task, err := CommitBackgroundCleanupPreviewRun(ctx, nil, who, uuid.New(), backgroundCleanupParamsFixture(), PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err != context.Canceled || task != uuid.Nil {
		t.Fatal("lost cancellation", err)
	}
}

func TestBackgroundCleanupCommitRejectsChangedCanonicalParamsBeforeDatabase(t *testing.T) {
	params := backgroundCleanupParamsFixture()
	raw, _, err := FreezeBackgroundCleanupPreviewParams(params)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, body := range [][]byte{nil, append(append([]byte(nil), raw...), ' '), []byte("{}")} {
		task, err := CommitBackgroundCleanupPreviewRun(context.Background(), &pgxpool.Pool{}, who, uuid.New(), params, PreviewCommitInputs{ParamsJCS: body}, nil, nil, nil, ExposureSourceDocuments{})
		if err != ErrInvalid || task != uuid.Nil {
			t.Fatal("changed params reached database", err)
		}
	}
}
