package interchange

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSchedulerExportCatalogParity(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL through migration02044 required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, signature := range []string{
		"photo.v30_export_adoption_root_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
		"photo.v30_export_adoption_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)",
		"photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)",
	} {
		var definition string
		if err := db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, signature).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(definition, "a.key") || !strings.Contains(definition, "a.object_key") {
			t.Fatalf("stale object-key ABI: %s", signature)
		}
	}
	var definition string
	if err := db.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`,
		"photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)").Scan(&definition); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"'FaceBoxes'", "'AbsoluteLUTArray'", "'DefectLabelMap'", "'application/x-npy'", "'StyleBackgroundAsset'"} {
		if !strings.Contains(definition, required) {
			t.Fatalf("missing catalog contract %s", required)
		}
	}
}
