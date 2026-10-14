package delivery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestCreateAdmissionServicePreflight(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	s := Service{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.AdmitPreparedCreate(ctx, who, i.tenant, i.Request(), i.key); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
	if err := s.AdmitPreparedCreate(context.Background(), who, uuid.Nil, i.Request(), i.key); err == nil {
		t.Fatal("nil tenant accepted")
	}
	if err := s.AdmitPreparedCreate(context.Background(), who, i.tenant, i.Request(), i.key); err == nil {
		t.Fatal("missing database accepted")
	}
}
