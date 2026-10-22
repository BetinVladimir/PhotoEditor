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
	var queue, generic, delegated, genericSHA, delegatedSHA string
	var scheduler, worker, delegatedPrivate bool
	err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.next_v23_identity_adoption(uuid)'::regprocedure),
 pg_get_functiondef('photo.v2_pending_adoptions()'::regprocedure),
 pg_get_functiondef('photo.v2_pending_adoptions_pre_relight_review_recovery()'::regprocedure),
 (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.v2_pending_adoptions()'::regprocedure),
 (SELECT encode(sha256(convert_to(prosrc,'UTF8')),'hex') FROM pg_proc WHERE oid='photo.v2_pending_adoptions_pre_relight_review_recovery()'::regprocedure),
 NOT EXISTS(SELECT 1 FROM unnest(ARRAY['photo_api','photo_worker','photo_scheduler',
  'photo_gateway','photo_portal','photo_maintenance']) role(name)
  WHERE has_function_privilege(role.name,
   'photo.v2_pending_adoptions_pre_relight_review_recovery()','EXECUTE')),
 has_function_privilege('photo_scheduler','photo.next_v23_identity_adoption(uuid)','EXECUTE'),
 has_function_privilege('photo_worker','photo.next_v23_identity_adoption(uuid)','EXECUTE')`).Scan(&queue, &generic, &delegated, &genericSHA, &delegatedSHA, &delegatedPrivate, &scheduler, &worker)
	if err != nil {
		t.Fatal(err)
	}
	if genericSHA != "f0efdceab45affe3f18d0947a34f89af8b5eb7d79a067b0aadcad61a5170826f" ||
		delegatedSHA != "66df3af3c8c64a3ef88570f7c321a1514cfd4e3bd01fa153d963669108e9398b" ||
		!scheduler || worker || !strings.Contains(queue, "t.operation='identity.compare'") ||
		!strings.Contains(queue, "adopted.consumer='v2-adopt'") ||
		!delegatedPrivate ||
		!strings.Contains(delegated, "t.operation<>'identity.compare'") ||
		!strings.Contains(generic, "WHERE task.operation='image.relight'") ||
		!strings.Contains(generic, "FROM photo.v2_pending_adoptions_pre_relight_review_recovery() prior") ||
		!strings.Contains(generic, "UNION ALL") {
		t.Fatal("identity queue isolation missing")
	}
}
