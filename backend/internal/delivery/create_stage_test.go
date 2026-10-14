package delivery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestCreateStageRejectsUnavailableDependenciesAndCancellation(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	s := Service{}
	limits := PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.StageCreatePrepare(ctx, who, i.tenant, i.Request(), i.key, "media", nil, limits); err != context.Canceled {
		t.Fatalf("cancelled stage: %v", err)
	}
	if _, err := s.StageCreatePrepare(context.Background(), who, uuid.Nil, i.Request(), i.key, "media", nil, limits); err == nil {
		t.Fatal("invalid scope accepted")
	}
	if _, err := s.StageCreatePrepare(context.Background(), who, i.tenant, i.Request(), i.key, "media", nil, limits); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	if _, err := s.ResolveCreatePrepare(ctx, who, i.tenant, i.Request(), i.key, "media", nil, limits); err != context.Canceled {
		t.Fatalf("cancelled resolver: %v", err)
	}
}
