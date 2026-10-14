package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2region"
	"strings"
	"testing"
	"time"
)

func TestIntegrationRegionAdmissionBuilder(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated database required")
	}
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture config")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	objects, err := objectstore.New(objectstore.Config{Endpoint: "http://127.0.0.1:1", Bucket: "test-region", AccessKey: "fixture", SecretKey: "fixture", Development: true})
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)
	evidence := strings.Repeat("f", 64)
	profile := v2region.RegionCalibration{Qualification: v2region.RegionQualification{RuntimeSHA: sha, ModelSHA: sha, ICCSHA: sha, DeviceSHA: sha, DatasetSHA: sha, PolicySHA: sha, Backend: "cuda"}, WorkloadSHA: sha, EvidenceSHA: evidence,
		Samples: []v2region.RegionCalibrationSample{{Width: 1, Height: 1, Peak: v2region.RegionOperatorPeak{VRAMBytes: 1}}}}
	manifest := map[string]any{"schema_version": 1, "profiles": []v2region.RegionCalibration{profile}}
	raw, _ := canonical.Marshal(manifest)
	pin, _ := canonical.Hash(manifest)
	catalog, err := v2region.NewPinnedRegionCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	queue, closeQueue, err := buildRegionAdmission(ctx, api, dsn, objects, "test-region", catalog, evidence)
	if err != nil || queue == nil {
		t.Fatal("Region not wired", err)
	}
	defer closeQueue()
	service, ok := queue.(v2region.QueueService)
	if !ok || service.DB != api || service.Scheduler == api || service.Scheduler.Config().MaxConns != 8 || service.Dependencies == nil {
		t.Fatal("roles/dependencies lost")
	}
	var role string
	if err = service.Scheduler.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "photo_scheduler" {
		t.Fatal("scheduler role", err)
	}
	if _, err = service.Queue(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), "region-disabled", v2region.RegionPreviewRequest{}); !errors.Is(err, v2region.ErrRegionUnavailable) {
		t.Fatal("disabled Region reached objects", err)
	}
	if _, err = admin.Exec(ctx, "ALTER FUNCTION photo.v26_region_seal_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) RENAME TO fixture_region_missing_scope"); err != nil {
		t.Fatal(err)
	}
	unavailable, closeUnavailable, err := buildRegionAdmission(ctx, api, dsn, objects, "test-region", catalog, evidence)
	defer closeUnavailable()
	if err == nil || unavailable != nil {
		t.Fatal("partial Region migrations accepted", err)
	}
}
