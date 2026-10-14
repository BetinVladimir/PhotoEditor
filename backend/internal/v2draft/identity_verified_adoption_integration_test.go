package v2draft

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationIdentityVerifiedAdoptionFunctionAndRoles(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	var scheduler, worker, api bool
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)'::regprocedure),
 has_function_privilege('photo_scheduler','photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)','EXECUTE'),
 has_function_privilege('photo_worker','photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)','EXECUTE'),
 has_function_privilege('photo_api','photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)','EXECUTE')`).Scan(&definition, &scheduler, &worker, &api)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"jsonb_array_length(p_inventory)<>4", "$/changed_mask/artifact",
		"i.contract='Calibration'", "adopt_v2_plan_pre_v23_identity"} {
		if !strings.Contains(definition, part) {
			t.Fatal("missing identity adoption gate", part)
		}
	}
	if !scheduler || worker || api {
		t.Fatal("adopter role exposure")
	}
}
