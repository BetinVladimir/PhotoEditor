package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
	"reflect"
	"testing"
	"time"
)

func TestIntegrationSemanticPreviewPublish(t *testing.T) {
	if os.Getenv("PHOTO_TEST_V27_PREVIEW_RUN") == "" {
		t.Skip("owned Apply preview fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("invalid fixture")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("database fixture unavailable")
	}
	defer db.Close()
	intent, err := (v2render.SQLRenderDerivativeIntents{DB: db}).Next(ctx, uuid.Nil)
	if err != nil || intent == nil || intent.Run.String() != os.Getenv("PHOTO_TEST_V27_PREVIEW_RUN") {
		t.Fatal("preview discovery mismatch", err)
	}
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	objects, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal("object fixture unavailable")
	}
	root, image, err := (RenderDerivativeLoader{DB: db, Objects: objects, Bucket: bucket}).Load(ctx, *intent)
	if err != nil {
		t.Fatal("source load failed", err)
	}
	tasks, err := PublishRenderDerivativeTasks(ctx, db, objects, bucket, *intent, root, image)
	if err != nil || len(tasks) != 2 {
		t.Fatal("publish failed", err)
	}
	replay, err := PublishRenderDerivativeTasks(ctx, db, objects, bucket, *intent, root, image)
	if err != nil || !reflect.DeepEqual(tasks, replay) {
		t.Fatal("publish replay mismatch", err)
	}
	working, err := review.ParseWorkingImage(image)
	if err != nil {
		t.Fatal("working image invalid", err)
	}
	coordinator := RenderDerivativeCoordinator{DB: db, Objects: objects, Bucket: bucket, WorkSHA: working.Color.ICCSHA256, SRGBSHA: intent.SRGBSHA}
	progressed, err := coordinator.ProcessOne(ctx)
	if err != nil || !progressed {
		t.Fatal("coordinator failed", err)
	}
	repeated, err := coordinator.ProcessOne(ctx)
	if err != nil || repeated {
		t.Fatal("coordinator replay unexpected", err)
	}
	if err := SealRenderDerivative(ctx, db, intent.Tenant, intent.Run, root, image); err != nil {
		t.Fatal("seal failed", err)
	}
	if err := SealRenderDerivative(ctx, db, intent.Tenant, intent.Run, root, image); err != nil {
		t.Fatal("seal replay failed", err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("owner fixture unavailable")
	}
	defer owner.Close()
	var worker uuid.UUID
	var node string
	if err = owner.QueryRow(ctx, "SELECT t.worker_id,w.node_id FROM photo.tasks t JOIN photo.worker_registrations w ON w.worker_id=t.worker_id WHERE t.tenant_id=$1 AND t.id=$2", intent.Tenant, intent.Task).Scan(&worker, &node); err != nil {
		t.Fatal("worker fixture missing", err)
	}
	if _, err = owner.Exec(ctx, `UPDATE photo.worker_registrations SET resource_classes='["cpu","media"]',qualified_operations=qualified_operations||'{"media.preview":["cpu"]}'::jsonb,last_seen=clock_timestamp() WHERE worker_id=$1`, worker); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, "UPDATE photo.resource_nodes SET reported_at=clock_timestamp() WHERE node_id=$1", node); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, `INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,'media1','media','{}'),($1,'media2','media','{}')`, node); err != nil {
		t.Fatal(err)
	}
	count, err := (Dispatcher{DB: db}).Dispatch(ctx, 2)
	if err != nil || count != 2 {
		t.Fatal("preview dispatch failed", count, err)
	}
	var targeted int
	if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='queued' AND dispatch_worker_id=$3 AND dispatch_command IS NOT NULL", intent.Tenant, intent.Run, worker).Scan(&targeted); err != nil || targeted != 2 {
		t.Fatal("preview targeting mismatch", targeted, err)
	}
	t.Log("V27_PREVIEW_PUBLISH_OK")
}
