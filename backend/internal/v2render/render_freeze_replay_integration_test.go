package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"testing"
)

// Saved-response fixture uses controlled SQL artifact metadata. It proves the
// concrete completed replay adapter, not initial storage/GPU execution.
func TestIntegrationRenderFreezeSavedReplay(t *testing.T) {
	raw := os.Getenv("PHOTO_RENDER_FREEZE_REPLAY")
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if raw == "" || dsn == "" {
		t.Skip("isolated saved freeze fixture required")
	}
	var fixture struct {
		Tenant, Actor, Session, Asset uuid.UUID
		Key                           string
		Request                       RenderFreezeRequest
	}
	if json.Unmarshal([]byte(raw), &fixture) != nil {
		t.Fatal("invalid replay fixture")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	defer admin.Close()
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	if err := admin.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", fixture.Actor).Scan(&who.Epoch); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid isolated database")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("API database unavailable")
	}
	defer api.Close()
	// Neither objects nor qualification are configured: completed replay cannot
	// accidentally depend on a later profile/model or object-store read.
	service := SQLRenderFreezeService{Drafts: v2draft.Service{DB: api}}
	response, err := service.Freeze(ctx, who, fixture.Tenant, fixture.Asset, fixture.Key, fixture.Request)
	if err != nil || response.Status != 200 || !response.Replayed {
		t.Fatal("saved freeze replay failed", err)
	}
	id, _ := RenderFreezeDocumentID(fixture.Tenant, fixture.Actor, fixture.Asset, fixture.Key, fixture.Request)
	if _, err := DecodeRenderFreezeResult(response.Body, id, fixture.Request); err != nil {
		t.Fatal("replay binding", err)
	}
	changed := fixture.Request
	changed.ExpectedRevision = "999"
	_, err = service.Freeze(ctx, who, fixture.Tenant, fixture.Asset, fixture.Key, changed)
	if domain.AsError(err).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("changed request did not conflict", err)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", fixture.Session); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Freeze(ctx, who, fixture.Tenant, fixture.Asset, fixture.Key, fixture.Request); err == nil {
		t.Fatal("revoked session received replay")
	}
}
