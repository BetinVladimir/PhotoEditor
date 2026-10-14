package v2input

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The isolated test database must be migrated through 02153. It checks the
// installed SECURITY DEFINER routing and the scheduler's real role ACL.
func TestIntegrationIdentityGenericAdoptionGuard(t *testing.T) {
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
	var generic, predecessor string
	var schedulerGeneric, schedulerPredecessor bool
	err = pool.QueryRow(ctx, `SELECT pg_get_functiondef('photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)'::regprocedure),
	 pg_get_functiondef('photo.adopt_v2_plan_pre_v23_identity(uuid,uuid,integer,bigint,uuid,jsonb,text)'::regprocedure),
	 has_function_privilege('photo_scheduler','photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE'),
	 has_function_privilege('photo_scheduler','photo.adopt_v2_plan_pre_v23_identity(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')`).
		Scan(&generic, &predecessor, &schedulerGeneric, &schedulerPredecessor)
	if err != nil {
		t.Fatal(err)
	}
	if !schedulerGeneric || schedulerPredecessor ||
		!strings.Contains(generic, "operation='identity.compare'") ||
		!strings.Contains(generic, "RETURN false") ||
		!strings.Contains(generic, "adopt_v2_plan_pre_v23_identity") ||
		!strings.Contains(predecessor, "adopt_v2_plan_pre_v31_signer") {
		t.Fatal("identity adoption bypass or predecessor gate missing")
	}
}
