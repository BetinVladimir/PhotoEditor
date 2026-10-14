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

func TestSchedulerExportDefectLabelFailsClosedWithoutClaim(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{}
	parent := v2input.Entry{Contract: "DefectCandidates"}
	array := v2input.Entry{Contract: "DefectLabelMap"}
	if err := catalog.QualifyExportDefectLabel(context.Background(), parent,
		[]byte(`{}`), array); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("defect edge without claim accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportDefectLabel(ctx, parent,
		[]byte(`{}`), array); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled defect qualification proceeded", err)
	}
}

func TestIntegrationSchedulerExportDefectLabelPrivateAndMissingClaim(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration 02052 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signature := "photo.v30_export_adoption_defect_label(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)"
	for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); err != nil || allowed != (role == "photo_scheduler") {
			t.Fatal("unexpected defect authority privilege", role, allowed, err)
		}
	}
	var allowed bool
	err = db.QueryRow(ctx, `SELECT photo.v30_export_adoption_defect_label(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"missing-claim", rawSHA([]byte("request")), uuid.New(), uuid.New(),
		rawSHA([]byte("source")), uuid.New(), rawSHA([]byte("proposal")),
		uuid.New(), rawSHA([]byte("labels"))).Scan(&allowed)
	if err != nil || allowed {
		t.Fatal("missing claim qualified defect labels", allowed, err)
	}
}
