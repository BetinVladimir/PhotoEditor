package provenance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This checks installation boundaries and invalid-call refusal only. Positive
// publication, quota races, revocation and cleanup need lifecycle fixtures.
func TestIntegrationSigningLineageStoragePermissionsAndInvalidScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	functions := []string{
		"photo.reserve_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)",
		"photo.complete_v31_signing_lineage(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text,uuid,text)",
		"photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)",
	}
	for _, signature := range functions {
		var secure, owned, api, worker, scheduler, gateway, portal, public bool
		err := db.QueryRow(ctx, `SELECT p.prosecdef,r.rolname='photo_fn_owner',has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),has_function_privilege('photo_gateway',p.oid,'EXECUTE'),has_function_privilege('photo_portal',p.oid,'EXECUTE'),EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`, signature).Scan(&secure, &owned, &api, &worker, &scheduler, &gateway, &portal, &public)
		if err != nil || !secure || !owned || !api || worker || scheduler || gateway || portal || public {
			t.Fatal("unsafe/missing lineage function", signature, err)
		}
	}
	var helper bool
	if err := db.QueryRow(ctx, `SELECT has_function_privilege('photo_api','photo.v31_signing_lineage_scope(uuid,uuid,uuid,uuid,jsonb,text)','EXECUTE')`).Scan(&helper); err != nil || helper {
		t.Fatal("private helper exposed", err)
	}
	var outcome string
	var artifact, token *uuid.UUID
	var ready bool
	err = db.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v31_signing_lineage($1,$2,$3,$4,'{}'::jsonb,$5,$6,$7,$8)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "invalid", int64(2), "bucket").Scan(&outcome, &artifact, &token, &ready)
	if err != nil || outcome != "invalid" || artifact != nil || token != nil || ready {
		t.Fatal("invalid scope reserved", err)
	}
	var accepted bool
	err = db.QueryRow(ctx, `SELECT photo.complete_v31_signing_lineage($1,$2,$3,$4,'{}'::jsonb,$5,$6,$7,$8,$9,$10)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "invalid", int64(2), "bucket", uuid.New(), "null").Scan(&accepted)
	if err != nil || accepted {
		t.Fatal("invalid completion accepted", err)
	}
	var version *string
	err = db.QueryRow(ctx, `SELECT photo.v31_ready_signing_lineage_version($1,$2,$3,$4,'{}'::jsonb,$5,$6,$7,$8)`, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "invalid", int64(2), "bucket").Scan(&version)
	if err != nil || version != nil {
		t.Fatal("invalid lineage replay version exposed", err, version)
	}
}
