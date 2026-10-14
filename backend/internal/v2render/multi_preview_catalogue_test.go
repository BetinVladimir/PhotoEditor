package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiPreviewCatalogueRejectsForeignScopeBeforeSQL(t *testing.T) {
	binding, task, _, _ := originalInventoryFixture(t, "optical.panorama")
	pin := MultiPreviewSource{Tenant: uuid.New(), Project: uuid.New(), Producer: task, Binding: binding, Attempt: 1, Fence: 2, Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}}
	catalogue := SQLMultiPreviewCatalogue{DB: &pgxpool.Pool{}, Source: pin, Bucket: "photos"}
	if !catalogue.scope(pin.Principal, pin.Tenant, pin.Project, task, binding.Operation, binding.Result, "photos") {
		t.Fatal("exact scope refused")
	}
	foreign := pin.Principal
	foreign.SessionID = uuid.New()
	if _, _, err := catalogue.LoadObject(context.Background(), foreign, pin.Tenant, pin.Project, task, binding.Operation, binding.Result, "photos"); err == nil {
		t.Fatal("foreign principal reached SQL")
	}
	if _, err := catalogue.LoadOriginalInventory(context.Background(), pin.Principal, pin.Tenant, uuid.New(), task, binding, "photos"); err == nil {
		t.Fatal("foreign project reached SQL")
	}
	if _, err := catalogue.LoadChildObject(context.Background(), pin.Principal, pin.Tenant, pin.Project, task, binding.Operation, binding.Result, binding.Result, "$/working_image", "WorkingImage", "photos"); err == nil {
		t.Fatal("root alias reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := catalogue.LoadObject(ctx, pin.Principal, pin.Tenant, pin.Project, task, binding.Operation, binding.Result, "photos"); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
