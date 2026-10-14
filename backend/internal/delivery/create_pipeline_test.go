package delivery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestCreatePipelinePreflight(t *testing.T) {
	i, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Service{}).PrepareDelivery(ctx, who, i.tenant, i.Request(), i.key, CreateDeliveryDependencies{}); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := (Service{}).PrepareDelivery(context.Background(), who, uuid.Nil, i.Request(), i.key, CreateDeliveryDependencies{}); err == nil {
		t.Fatal("invalid tenant accepted")
	}
	if _, err := (Service{}).PrepareDelivery(context.Background(), who, i.tenant, i.Request(), i.key, CreateDeliveryDependencies{}); err == nil {
		t.Fatal("unconfigured pipeline accepted")
	}
}
