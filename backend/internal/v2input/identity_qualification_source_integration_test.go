package v2input

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationIdentityQualificationSourceRoleScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var definition string
	var scheduler, api, worker bool
	err = pool.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)'::regprocedure),
 has_function_privilege('photo_scheduler','photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)','EXECUTE'),
 has_function_privilege('photo_api','photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)','EXECUTE'),
 has_function_privilege('photo_worker','photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)','EXECUTE')`).Scan(&definition, &scheduler, &api, &worker)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"policy.admission_enabled", "qualified.trusted_import",
		"qualified.status='passed'", "p_calibration_sha=ANY(policy.calibration_shas)",
		"candidate.status='passed'", "worker.qualified_operations->'identity.compare'",
		"report.object_version NOT IN('','null')"} {
		if !strings.Contains(definition, required) {
			t.Fatal("missing qualification source gate", required)
		}
	}
	if !scheduler || api || worker {
		t.Fatal("identity qualification source role exposure")
	}
}
