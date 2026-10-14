package v2draft

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationIdentityAdoptionQueueIsolation(t *testing.T) {
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
	var queue, generic string
	var scheduler, worker bool
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.next_v23_identity_adoption(uuid)'::regprocedure),
 pg_get_functiondef('photo.v2_pending_adoptions()'::regprocedure),
 has_function_privilege('photo_scheduler','photo.next_v23_identity_adoption(uuid)','EXECUTE'),
 has_function_privilege('photo_worker','photo.next_v23_identity_adoption(uuid)','EXECUTE')`).Scan(&queue, &generic, &scheduler, &worker)
	if err != nil {
		t.Fatal(err)
	}
	if !scheduler || worker || !strings.Contains(queue, "t.operation='identity.compare'") ||
		!strings.Contains(queue, "adopted.consumer='v2-adopt'") ||
		!strings.Contains(generic, "t.operation<>'identity.compare'") {
		t.Fatal("identity queue isolation missing")
	}
}
