package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupSavedRunRejectsScopeBeforeIO(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if id, err := SaveBackgroundCleanupPreview(context.Background(), nil, who, backgroundCleanupParamsFixture()); err == nil || id != uuid.Nil {
		t.Fatal("nil repository accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if id, err := SaveBackgroundCleanupPreview(ctx, nil, who, backgroundCleanupParamsFixture()); err != context.Canceled || id != uuid.Nil {
		t.Fatal("cancelled save accepted", err)
	}
}
