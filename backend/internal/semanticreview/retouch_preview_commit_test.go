package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRetouchCommitRejectsUnscopedOrCancelledCall(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if task, err := CommitRetouchPreviewRun(context.Background(), nil, who, uuid.New(), retouchParamsFixture(), PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err == nil || task != uuid.Nil {
		t.Fatal("unscoped commit accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if task, err := CommitRetouchPreviewRun(ctx, nil, who, uuid.New(), retouchParamsFixture(), PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err != context.Canceled || task != uuid.Nil {
		t.Fatal("lost cancellation", err)
	}
}

func TestRetouchCommitRejectsChangedCanonicalParamsBeforeDatabase(t *testing.T) {
	params := retouchParamsFixture()
	raw, _, err := FreezeRetouchPreviewParams(params)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, body := range [][]byte{nil, append(append([]byte(nil), raw...), ' '), []byte("{}")} {
		task, err := CommitRetouchPreviewRun(context.Background(), &pgxpool.Pool{}, who, uuid.New(), params, PreviewCommitInputs{ParamsJCS: body}, nil, nil, nil, ExposureSourceDocuments{})
		if err != ErrInvalid || task != uuid.Nil {
			t.Fatal("changed params reached database", err)
		}
	}
}
