package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPreviewCommitRejectsCancelledAndIncompleteInputsBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := previewParamsFixture()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if id, err := CommitExposurePreviewRun(ctx, &pgxpool.Pool{}, who, uuid.New(), p, PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err != context.Canceled || id != uuid.Nil {
		t.Fatal("cancel ignored", err)
	}
	if _, err := CommitExposurePreviewRun(context.Background(), nil, who, uuid.New(), p, PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := CommitExposurePreviewRun(context.Background(), &pgxpool.Pool{}, who, uuid.New(), p, PreviewCommitInputs{}, nil, nil, nil, ExposureSourceDocuments{}); err == nil {
		t.Fatal("unbound commit reached database")
	}
}
