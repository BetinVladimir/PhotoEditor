package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRemovalQuotaRejectsMissingScopeOrCancellation(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if accepted, err := ReserveRemovalPreviewQuota(context.Background(), nil, who, uuid.New(), FrozenRemovalPreviewTask{}, nil); accepted || err == nil {
		t.Fatal("unscoped quota accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if accepted, err := ReserveRemovalPreviewQuota(ctx, nil, who, uuid.New(), FrozenRemovalPreviewTask{}, nil); accepted || err != context.Canceled {
		t.Fatal("lost cancellation", err)
	}
}
