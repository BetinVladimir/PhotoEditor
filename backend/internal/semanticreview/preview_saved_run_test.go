package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestSavePreviewRejectsCancelledAndInvalidScopeBeforeDatabase(t *testing.T) {
	p := previewParamsFixture()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if id, err := SaveExposurePreview(ctx, &pgxpool.Pool{}, who, p); err != context.Canceled || id != uuid.Nil {
		t.Fatal("cancel ignored", err)
	}
	if id, err := SaveExposurePreview(context.Background(), nil, who, p); err == nil || id != uuid.Nil {
		t.Fatal("nil database accepted")
	}
	if _, err := SaveExposurePreview(context.Background(), &pgxpool.Pool{}, auth.Identity{}, p); err == nil {
		t.Fatal("anonymous editor accepted")
	}
	p.TargetRevision = "01"
	if _, err := SaveExposurePreview(context.Background(), &pgxpool.Pool{}, who, p); err == nil {
		t.Fatal("invalid params reached database")
	}
}
