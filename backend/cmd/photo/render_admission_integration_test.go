package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
	"testing"
	"time"
)

func TestIntegrationRenderAdmissionBuilder(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB required")
	}
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture config unavailable")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	objects, err := objectstore.New(objectstore.Config{Endpoint: "http://127.0.0.1:1", Bucket: "test-render", AccessKey: "fixture", SecretKey: "fixture", Development: true})
	if err != nil {
		t.Fatal(err)
	}
	queue, closeQueue, err := buildRenderAdmission(ctx, api, dsn, objects, "test-render", nil)
	if err != nil || queue == nil {
		t.Fatal("complete boundary not wired", err)
	}
	defer closeQueue()
	service, ok := queue.(v2render.SQLRenderSubmissionService)
	if !ok || service.Factory.API != api || service.Factory.Scheduler == api || service.Factory.Scheduler.Config().MaxConns != 8 || service.Calibrations != nil || service.Timeout != 2*time.Minute {
		t.Fatal("unexpected service dependencies")
	}
	var apiRole, schedulerRole string
	if err := api.QueryRow(ctx, "SELECT current_user").Scan(&apiRole); err != nil {
		t.Fatal(err)
	}
	if err := service.Factory.Scheduler.QueryRow(ctx, "SELECT current_user").Scan(&schedulerRole); err != nil {
		t.Fatal(err)
	}
	if apiRole != "photo_api" || schedulerRole != "photo_scheduler" {
		t.Fatal("role separation lost")
	}
	_, err = admin.Exec(ctx, "ALTER FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) RENAME TO fixture_missing_v26_completion")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "ALTER FUNCTION photo.fixture_missing_v26_completion(uuid,uuid,uuid,uuid,text,text) RENAME TO complete_v26_render_submission"); err != nil {
			t.Error(err)
		}
	}()
	disabled, closeDisabled, err := buildRenderAdmission(ctx, api, dsn, objects, "test-render", nil)
	defer closeDisabled()
	if err != nil || disabled != nil {
		t.Fatal("partial migration boundary enabled", err)
	}
}
