package v2draft

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationReadyDraftVersionReaderIsPrivateAndScopeBound(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration 01888 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var owner string
	var securityDefiner, apiAllowed, workerAllowed, publicAllowed bool
	err = db.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef,
 has_function_privilege('photo_api',p.oid,'EXECUTE'),
 has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.v21_ready_draft_object(uuid,uuid,uuid,uuid,uuid,text,text)')`).
		Scan(&owner, &securityDefiner, &apiAllowed, &workerAllowed, &publicAllowed)
	if err != nil || owner != "photo_fn_owner" || !securityDefiner || !apiAllowed || workerAllowed || publicAllowed {
		t.Fatal("ready draft reader installation or grants invalid", err)
	}
	var count int
	err = db.QueryRow(ctx, `SELECT count(*) FROM photo.v21_ready_draft_object($1,$2,$3,$4,$5,$6,$7)`,
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "test").Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("fabricated ready draft object resolved", err, count)
	}
}
