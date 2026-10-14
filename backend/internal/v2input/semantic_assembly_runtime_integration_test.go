package v2input

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

// Caller owns the isolated database and bucket. This invokes real publication.
func TestIntegrationV27AssemblyRuntime(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" || os.Getenv("PHOTO_TEST_V27_ASSEMBLY_RUN") == "" {
		t.Skip("isolated assembly fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	storage, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := (SemanticAssemblyRuntime{DB: pool, Objects: storage, Bucket: bucket}).Orchestrator()
	if err != nil {
		t.Fatal(err)
	}
	processed, err := runtime.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !processed {
		t.Fatal("assembly was not published")
	}
	t.Log("V27_ASSEMBLY_RUNTIME_OK")
}
