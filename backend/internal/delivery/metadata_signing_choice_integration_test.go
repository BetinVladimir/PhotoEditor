package delivery

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigratedMetadataStagesUseFrozenSigningChoice(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" { t.Skip("isolated migrated PostgreSQL required") }
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	for _, signature := range []string{
		"photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)",
		"photo.adopt_v30_delivery_metadata(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint)",
	} {
		var definition string
		if err := db.QueryRow(ctx, `SELECT pg_get_functiondef(to_regprocedure($1))`,
			signature).Scan(&definition); err != nil { t.Fatal(err) }
		if !strings.Contains(definition, "v31_delivery_frozen_signing_choice_valid") ||
			strings.Contains(definition, "provenance' IS DISTINCT FROM 'off'") {
			t.Fatalf("metadata signing branch not installed: %s", signature)
		}
	}
}
