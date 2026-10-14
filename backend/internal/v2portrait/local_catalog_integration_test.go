package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

// These installed-function denial checks require no tenant writes. Positive
// producer/owner fixtures are still required for complete admission acceptance.
func TestLocalPortraitPostgresCatalogDenials(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PHOTO_TEST_DATABASE_URL and migrations through 00844 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("cannot connect to test database")
	}
	defer conn.Close(ctx)
	scheduler, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scheduler.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		scheduler.Rollback(ctx)
		t.Fatal(err)
	}
	var count int
	if err = scheduler.QueryRow(ctx, "SELECT count(*) FROM photo.read_v28_local_recovery_owner(NULL,NULL,NULL,NULL)").Scan(&count); err != nil || count != 0 {
		scheduler.Rollback(ctx)
		t.Fatal("null owner context not denied", err)
	}
	absent := uuid.New()
	if err = scheduler.QueryRow(ctx, "SELECT count(*) FROM photo.read_v28_local_recovery_owner($1,$2,$3,$4)", absent, uuid.New(), uuid.New(), uuid.New()).Scan(&count); err != nil || count != 0 {
		scheduler.Rollback(ctx)
		t.Fatal("missing owner context not denied", err)
	}
	if err = scheduler.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	api, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Rollback(ctx)
	if _, err = api.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if _, err = api.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", absent.String()); err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"Params", "RasterizedMask", "ManualMask", "MaskPixels", "Artifact"} {
		for _, parent := range []any{nil, uuid.New()} {
			var allowed bool
			if err = api.QueryRow(ctx, "SELECT photo.v2_draft_artifact_scope($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", absent, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), contract, uuid.New(), uuid.New(), parent, strings.Repeat("b", 64)).Scan(&allowed); err != nil || allowed {
				t.Fatal("unauthorized catalog occurrence accepted", contract, err)
			}
		}
	}
}
