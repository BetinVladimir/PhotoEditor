package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Checks the installed boundary, not the migration text: ordinary discovery
// must exclude watermark children and bind every candidate to its frozen slot.
func TestIntegrationDeliveryPendingChildOwnership(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through 01959 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v30_delivery_pending_child_slots(uuid,uuid,integer)'::regprocedure)`).Scan(&definition)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"jsonb_typeof",
		"config_jcs",
		"watermark",
		"v30_delivery_child_request",
		"IS NOT DISTINCT FROM",
		"app.tenant_id",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("installed ordinary observer lacks %q", required)
		}
	}
}
