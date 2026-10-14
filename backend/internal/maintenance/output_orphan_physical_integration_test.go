//go:build integration

package maintenance

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

func TestIntegrationOutputOrphanPhysicalCleanup(t *testing.T) {
	if os.Getenv("PHOTO_TEST_ORPHAN_PHYSICAL") != "1" {
		t.Skip("isolated fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET"), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	cleaner := OutputCleaner{Repository: SQLCleanup{DB: db}, Objects: store, Bucket: os.Getenv("PHOTO_TEST_MINIO_BUCKET")}
	worked, err := cleaner.CleanOne(ctx)
	if err != nil || !worked {
		t.Fatal("physical cleanup", worked, err)
	}
	t.Log("OUTPUT_ORPHAN_PHYSICAL_CLEANED_OK")
}
