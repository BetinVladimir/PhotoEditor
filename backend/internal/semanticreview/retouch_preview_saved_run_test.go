package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchSavedRunRejectsScopeBeforeIO(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if id, err := SaveRetouchPreview(context.Background(), nil, who, retouchParamsFixture(), v2input.Ref{}, nil, nil); err == nil || id != uuid.Nil {
		t.Fatal("nil repository accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if id, err := SaveRetouchPreview(ctx, nil, who, retouchParamsFixture(), v2input.Ref{}, nil, nil); err != context.Canceled || id != uuid.Nil {
		t.Fatal("cancelled save accepted", err)
	}
}
