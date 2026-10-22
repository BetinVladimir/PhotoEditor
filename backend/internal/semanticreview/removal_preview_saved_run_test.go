package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRemovalSavedRunRejectsScopeBeforeIO(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if id, err := SaveRemovalPreview(context.Background(), nil, who, removalParamsFixture()); err == nil || id != uuid.Nil {
		t.Fatal("nil repository accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if id, err := SaveRemovalPreview(ctx, nil, who, removalParamsFixture()); err != context.Canceled || id != uuid.Nil {
		t.Fatal("cancelled save accepted", err)
	}
}
