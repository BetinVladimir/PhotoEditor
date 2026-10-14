package v2draft

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationIdentityDispatchRequiresAdopterAndQualification(t *testing.T) {
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
	var configure, activate string
	var maintenance, scheduler, workerHeartbeat bool
	err = db.QueryRow(ctx, `SELECT
 pg_get_functiondef('photo.configure_v23_identity_dispatch(boolean,uuid)'::regprocedure),
 pg_get_functiondef('photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)'::regprocedure),
 has_function_privilege('photo_maintenance','photo.configure_v23_identity_dispatch(boolean,uuid)','EXECUTE'),
 has_function_privilege('photo_scheduler','photo.configure_v23_identity_dispatch(boolean,uuid)','EXECUTE'),
 has_function_privilege('photo_worker','photo.v23_identity_adopter_heartbeat()','EXECUTE')`).Scan(&configure, &activate, &maintenance, &scheduler, &workerHeartbeat)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"v23_identity_adopter_state", "qualified.trusted_import", "report.state='ready'", "worker.qualified_operations->'identity.compare'", "v2_enabled=p_enable"} {
		if !strings.Contains(configure, fragment) {
			t.Fatal("missing activation gate", fragment)
		}
	}
	if !strings.Contains(activate, "v23_identity_adopter_state") || !maintenance || scheduler || workerHeartbeat {
		t.Fatal("identity dispatch role or live-adopter fence invalid")
	}
}
