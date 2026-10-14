package interchange

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportWorkingWarpFailsClosedWithoutClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{}
	parent := v2input.Entry{Contract: "WorkingImage"}
	field := v2input.Entry{Contract: "DenseWarpField"}
	if err := catalog.QualifyExportDenseWarpOccurrence(context.Background(), parent,
		[]byte(`{}`), 0, field); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("warp without claim accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportDenseWarpOccurrence(ctx, parent,
		[]byte(`{}`), 0, field); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled warp qualification proceeded", err)
	}
}

func TestIntegrationSchedulerExportWorkingWarpPrivateAndMissingClaim(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration 02049 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.v30_export_adoption_working_warp(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text)"
	for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
			t.Fatal("unexpected warp authority privilege", role, allowed, err)
		}
	}
	var allowed bool
	err = db.QueryRow(ctx, `SELECT photo.v30_export_adoption_working_warp(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"missing-claim", rawSHA([]byte("request")), uuid.New(), uuid.New(),
		rawSHA([]byte("parent")), uuid.New(), rawSHA([]byte("field"))).Scan(&allowed)
	if err != nil || allowed {
		t.Fatal("missing claim qualified warp", allowed, err)
	}
}
