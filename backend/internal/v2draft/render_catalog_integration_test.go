package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

func TestIntegrationRenderCatalogFrameAuthority(t *testing.T) {
	path := os.Getenv("PHOTO_RENDER_CATALOG_FIXTURE")
	if path == "" {
		t.Skip("isolated render authority fixture required")
	}
	var f struct {
		Tenant, Project, Version, Analysis, Actor, Session uuid.UUID
		Root, Descriptor, Source, Faces, Pixels, Alpha     v2input.Ref
		Epoch                                              int64
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	catalog := SQLArtifactCatalog{DB: pool, Bucket: "test", ProjectID: f.Project, AssetVersionID: f.Version}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session, Epoch: f.Epoch}
	binding := v2input.SessionVersionBinding{AssetVersionID: f.Version, Source: f.Source, CanvasHash: strings.Repeat("b", 64)}
	frame := RenderFrameAuthority{Root: f.Root, Descriptor: f.Descriptor, SelectedVersion: f.Version, Learning: FrozenRenderLearning{Descriptor: f.Descriptor, AnalysisID: f.Analysis, Ordinal: 0}, ModelLockSHA: strings.Repeat("e", 64), Ordinal: 0}
	nodes := []LayerRef{
		{Contract: "FrozenRenderRecipe", Ref: f.Root},
		{Contract: "WorkingImage", Ref: f.Source, ParentRef: f.Descriptor, ExpectedBinding: &binding, RenderFrame: &frame},
		{Contract: "FaceObservations", Ref: f.Faces, ParentRef: f.Descriptor, ExpectedBinding: &binding, RenderFrame: &frame},
		{Contract: "WorkingPixels", Ref: f.Pixels, ParentRef: f.Source, ExpectedBinding: &binding, RenderFrame: &frame},
		{Contract: "WorkingAlpha", Ref: f.Alpha, ParentRef: f.Source, ExpectedBinding: &binding, RenderFrame: &frame},
	}
	for _, node := range nodes {
		obj, err := catalog.Lookup(ctx, who, f.Tenant, node)
		if err != nil || obj.SHA256 != node.Ref.SHA256 || obj.VersionID == "" || obj.Bytes < 1 {
			t.Fatalf("%s lookup: %v %+v", node.Contract, err, obj)
		}
		bad := node
		bad.Ref.SHA256 = strings.Repeat("f", 64)
		if _, err = catalog.Lookup(ctx, who, f.Tenant, bad); err == nil {
			t.Fatalf("%s wrong SHA accepted", node.Contract)
		}
	}
	wrong := frame
	wrong.Ordinal = 1
	for _, node := range nodes[1:] {
		node.RenderFrame = &wrong
		if _, err = catalog.Lookup(ctx, who, f.Tenant, node); err == nil {
			t.Fatalf("%s absent frame accepted", node.Contract)
		}
	}
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err = admin.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", f.Session); err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if _, err = catalog.Lookup(ctx, who, f.Tenant, node); err == nil {
			t.Fatalf("%s revoked session accepted", node.Contract)
		}
	}

}
