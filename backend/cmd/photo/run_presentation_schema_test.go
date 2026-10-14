package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestIntegrationRunPresentationSchema(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	if err = requireRunPresentationSchema(ctx, api); err != nil {
		t.Fatal(err)
	}
	if requireRunPresentationSchema(ctx, admin) == nil {
		t.Fatal("wrong runtime role accepted")
	}
	if _, err = admin.Exec(ctx, "ALTER FUNCTION photo.v26_render_presentation_status(uuid,uuid,uuid,uuid) RENAME TO fixture_hidden_presentation_status"); err != nil {
		t.Fatal(err)
	}
	absent := requireRunPresentationSchema(ctx, api)
	if _, err = admin.Exec(ctx, "ALTER FUNCTION photo.fixture_hidden_presentation_status(uuid,uuid,uuid,uuid) RENAME TO v26_render_presentation_status"); err != nil {
		t.Fatal(err)
	}
	if absent == nil {
		t.Fatal("missing function accepted")
	}
	if _, err = admin.Exec(ctx, "REVOKE EXECUTE ON FUNCTION photo.v26_render_presentation_status(uuid,uuid,uuid,uuid) FROM photo_api"); err != nil {
		t.Fatal(err)
	}
	denied := requireRunPresentationSchema(ctx, api)
	if _, err = admin.Exec(ctx, "GRANT EXECUTE ON FUNCTION photo.v26_render_presentation_status(uuid,uuid,uuid,uuid) TO photo_api"); err != nil {
		t.Fatal(err)
	}
	if denied == nil {
		t.Fatal("missing execute privilege accepted")
	}
	if err = requireRunPresentationSchema(ctx, api); err != nil {
		t.Fatal(err)
	}
}
