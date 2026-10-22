package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestPreviewInspectionRejectsInvalidScopeBeforeDatabase(t *testing.T) {
	p := previewParamsFixture()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := InspectExposurePreviewRun(ctx, &pgxpool.Pool{}, who, uuid.New(), p)
	if err != context.Canceled || got.RunID != uuid.Nil {
		t.Fatal("cancel ignored", got, err)
	}
	if _, err := InspectExposurePreviewRun(context.Background(), nil, who, uuid.New(), p); err == nil {
		t.Fatal("nil database accepted")
	}
	p.TargetRevision = "01"
	if _, err := InspectExposurePreviewRun(context.Background(), &pgxpool.Pool{}, who, uuid.New(), p); err == nil {
		t.Fatal("invalid params reached database")
	}
}
