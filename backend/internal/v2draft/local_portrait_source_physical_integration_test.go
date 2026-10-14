package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationLocalPortraitSourcePhysical(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_LOCAL_SOURCE")
	if raw == "" {
		t.Skip("isolated fixture required")
	}
	var f struct {
		Tenant, Project, Actor, Session uuid.UUID
		Binding                         v2input.SessionVersionBinding
		Source                          string
		Failure                         string
		Objects                         objectstore.Config
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	resolver := ClosureResolver{Catalog: SQLArtifactCatalog{DB: db, Bucket: f.Objects.Bucket, ProjectID: f.Project, AssetVersionID: f.Binding.AssetVersionID}, Objects: store}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	body, err := resolver.ReadLocalPortraitSource(ctx, who, f.Tenant, f.Binding)
	if f.Failure != "" {
		if err == nil || body != nil {
			t.Fatal("unavailable or corrupt pinned version returned source", f.Failure)
		}
		t.Log("LOCAL_PORTRAIT_PINNED_SOURCE_REFUSED", f.Failure)
		return
	}
	if err != nil || string(body) != f.Source {
		t.Fatal("pinned physical source read", err)
	}
	bad := f.Binding
	bad.CanvasHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err = resolver.ReadLocalPortraitSource(ctx, who, f.Tenant, bad); err == nil {
		t.Fatal("wrong canvas accepted")
	}
	if _, err = resolver.ReadLocalPortraitSource(ctx, who, uuid.New(), f.Binding); err == nil {
		t.Fatal("foreign tenant accepted")
	}
	who.SessionID = uuid.New()
	if _, err = resolver.ReadLocalPortraitSource(ctx, who, f.Tenant, f.Binding); err == nil {
		t.Fatal("foreign session accepted")
	}
	t.Log("LOCAL_PORTRAIT_PINNED_SOURCE_PHYSICAL_OK")
}
