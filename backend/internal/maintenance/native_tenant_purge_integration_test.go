//go:build integration

package maintenance

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

func TestIntegrationNativeRegistryTenantPurge(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_NATIVE_TENANT_PURGE")
	if raw == "" {
		t.Skip("isolated fixture required")
	}
	var fixture struct {
		Tenant   uuid.UUID
		Neighbor objectstore.Object
	}
	if json.Unmarshal([]byte(raw), &fixture) != nil || fixture.Tenant == uuid.Nil {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	owner, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatal("owner connection")
	}
	defer owner.Close()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("maintenance connection")
	}
	defer pool.Close()
	storage, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("storage configuration")
	}
	cleaner := TenantCleaner{Repository: SQLTenantDeletion{DB: pool}, Objects: storage, Batch: 1}
	for pass := 0; pass < 16; pass++ {
		changed, err := cleaner.CleanOne(ctx)
		if err != nil || !changed {
			t.Fatal("actual cleaner", pass, err)
		}
		var status string
		var rows int
		if err := owner.QueryRow(ctx, "SELECT status FROM photo.tenants WHERE id=$1", fixture.Tenant).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.semantic_native_protections WHERE tenant_id=$1", fixture.Tenant).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if err := storage.Check(ctx, fixture.Neighbor); err != nil {
			t.Fatal("neighbor tenant object damaged", err)
		}
		if pass == 0 && (status != "deleting" || rows != 1) {
			t.Fatal("metadata purged before bounded object cleanup", status, rows)
		}
		if status == "deleted" {
			if rows != 0 || pass < 2 {
				t.Fatal("invalid final purge", rows, pass)
			}
			var passes int64
			if err := owner.QueryRow(ctx, "SELECT cleanup_passes FROM photo.deletion_jobs WHERE tenant_id=$1 AND resource_type='tenant'", fixture.Tenant).Scan(&passes); err != nil || passes < 3 {
				t.Fatal("missing checkpoints", passes, err)
			}
			more, err := storage.PurgeTenantBatch(ctx, fixture.Tenant, 1)
			if err != nil || more {
				t.Fatal("prefix not empty after metadata purge", err)
			}
			t.Log("V27_NATIVE_REGISTRY_PHYSICAL_TENANT_PURGE_OK")
			return
		}
	}
	t.Fatal("tenant cleanup did not converge")
}
