package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestIntegrationSemanticCreateDeployment(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_V27_CREATE_DEPLOYMENT")
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if raw == "" || dsn == "" {
		t.Skip("owned physical create fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	var fixture struct {
		Objects                objectstore.Config
		Tenant, Actor, Session uuid.UUID
		Intent                 v2input.SemanticEditIntent
	}
	if json.Unmarshal([]byte(os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")), &fixture) != nil {
		t.Fatal("create fixture invalid")
	}
	store, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("create fixture storage unavailable")
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("create fixture database configuration unavailable")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("create fixture API unavailable")
	}
	t.Cleanup(api.Close)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(filepath.Join(root, "work"), "v27-create-startup-")
	if err != nil {
		t.Fatal("create fixture directory unavailable")
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	path := filepath.Join(directory, "deployment.json")
	if os.WriteFile(path, []byte(raw), 0600) != nil {
		t.Fatal("create fixture configuration unavailable")
	}
	t.Setenv("PHOTO_V27_CREATE_CONFIG", path)
	t.Setenv("PHOTO_ICC_ROOT", filepath.Join(root, "resources", "icc"))
	roleDSN := func(role string) string {
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatal("create fixture DSN invalid")
			}
			query := u.Query()
			query.Set("options", "-c role="+role)
			u.RawQuery = query.Encode()
			return u.String()
		}
		return dsn + " options='-c role=" + role + "'"
	}
	t.Setenv("V27_CREATE_SCHEDULER_DATABASE_URL", roleDSN("photo_scheduler"))
	service, closeService, err := buildSemanticCreate(ctx, api, store, fixture.Objects.Bucket)
	if err != nil || service == nil || closeService == nil {
		t.Fatal("configured create installation failed", err)
	}
	closeService()
	var role string
	if err := api.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "photo_api" {
		t.Fatal("create close damaged caller API pool")
	}
	probeSemanticCreateExecutable(t, ctx, root, directory, path, dsn, roleDSN("photo_scheduler"), fixture.Objects, fixture.Tenant, fixture.Actor, fixture.Session, fixture.Intent)
	// The same valid deployment cannot silently use API credentials to schedule.
	t.Setenv("V27_CREATE_SCHEDULER_DATABASE_URL", roleDSN("photo_api"))
	service, closeService, err = buildSemanticCreate(ctx, api, store, fixture.Objects.Bucket)
	if err == nil || service != nil || closeService == nil {
		t.Fatal("create startup accepted wrong scheduler role")
	}
	closeService()
	t.Setenv("V27_CREATE_SCHEDULER_DATABASE_URL", roleDSN("photo_scheduler"))
	if os.WriteFile(path, []byte(`{"schema_version":1,"projects":[]}`), 0600) != nil {
		t.Fatal("create fixture malformed configuration unavailable")
	}
	service, closeService, err = buildSemanticCreate(ctx, api, store, fixture.Objects.Bucket)
	if err == nil || service != nil || closeService == nil {
		t.Fatal("create startup ignored malformed configuration")
	}
	closeService()
	t.Log("V27_CREATE_DEPLOYMENT_BUILD_OK")
}
