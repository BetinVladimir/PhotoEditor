//go:build integration

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type nativeGCLeaseProbe struct {
	store            *objectstore.Store
	api              *pgxpool.Pool
	tenant, artifact uuid.UUID
	refused          bool
}

func (p *nativeGCLeaseProbe) RemoveConfirmed(ctx context.Context, object objectstore.Object) error {
	tx, err := p.api.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", p.tenant.String()); err != nil {
		return err
	}
	var state string
	if err = tx.QueryRow(ctx, "SELECT state FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", p.tenant, p.artifact).Scan(&state); err != nil {
		return err
	}
	if state != "deleting" {
		return errors.New("GC did not claim deleting artifact")
	}
	_, err = tx.Exec(ctx, "INSERT INTO photo.artifact_leases(tenant_id,artifact_id,holder_type,holder_id,expires_at) VALUES($1,$2,'gc-boundary',$3,clock_timestamp()+interval '1 minute')", p.tenant, p.artifact, uuid.New())
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" {
		return errors.New("deleting artifact accepted a read lease")
	}
	p.refused = true
	return p.store.RemoveConfirmed(ctx, object)
}

func TestIntegrationV27NativeArtifactPhysicalGC(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_NATIVE_ARTIFACT_GC")
	if raw == "" {
		t.Skip("isolated fixture required")
	}
	var f struct {
		Tenant, Artifact        uuid.UUID
		Remove                  bool
		Target, Sibling, Report objectstore.Object
	}
	if json.Unmarshal([]byte(raw), &f) != nil || f.Tenant == uuid.Nil || f.Artifact == uuid.Nil {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	maintenance, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.Close()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	objects := &nativeGCLeaseProbe{store: store, api: api, tenant: f.Tenant, artifact: f.Artifact}
	cleaner := ArtifactCleaner{Repository: SQLArtifactGC{DB: maintenance}, Objects: objects, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET")}
	changed, err := cleaner.CleanOne(ctx)
	if err != nil || changed != f.Remove {
		t.Fatal("actual artifact cleaner", changed, err)
	}
	for _, object := range []objectstore.Object{f.Sibling, f.Report} {
		if err := store.Check(ctx, object); err != nil {
			t.Fatal("unrelated exact version damaged", err)
		}
	}
	if !f.Remove {
		if objects.refused {
			t.Fatal("protected object reached remover")
		}
		if err := store.Check(ctx, f.Target); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_NATIVE_ARTIFACT_PHYSICAL_RETAINED_OK")
		return
	}
	if !objects.refused {
		t.Fatal("missing real API lease refusal")
	}
	var state string
	var token uuid.UUID
	if err := owner.QueryRow(ctx, "SELECT state,gc_token FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", f.Tenant, f.Artifact).Scan(&state, &token); err != nil || state != "deleted" {
		t.Fatal("GC metadata completion", state, err)
	}
	var replay, stale bool
	if err := maintenance.QueryRow(ctx, "SELECT photo.finish_artifact_gc($1,$2)", f.Artifact, token).Scan(&replay); err != nil || !replay {
		t.Fatal("GC finish replay", err)
	}
	if err := maintenance.QueryRow(ctx, "SELECT photo.finish_artifact_gc($1,$2)", f.Artifact, uuid.New()).Scan(&stale); err != nil || stale {
		t.Fatal("foreign GC token accepted", err)
	}
	if again, err := cleaner.CleanOne(ctx); err != nil || again {
		t.Fatal("deleted artifact reclaimed", again, err)
	}
	t.Log("V27_NATIVE_ARTIFACT_PHYSICAL_GC_OK")
}
