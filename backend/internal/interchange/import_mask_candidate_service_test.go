package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestImportPersonMaskCandidateServiceRequiresCurrentScopedDependencies(t *testing.T) {
	service := ImportPersonMaskCandidateService{}
	id := uuid.New()
	who := auth.Identity{UserID: id, SessionID: uuid.New()}
	if _, err := service.ReadEntry(context.Background(), who, id, id, id, id); err == nil {
		t.Fatal("accepted candidate read without SQL and object authorities")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ReadEntry(ctx, who, id, id, id, id); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
}
