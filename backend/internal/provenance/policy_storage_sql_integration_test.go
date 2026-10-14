package provenance

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Negative authority/privilege checks only; not proof of a signing lifecycle.
// Requires isolated admin database migrated through01887. Never run against production.
func TestIntegrationSigningPolicyPublicationAuthority(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	tenant, policy := uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	artifact := uuid.NewSHA1(tenant, []byte("signing:policy:"+policy.String()+":"+sha))
	for _, test := range []struct {
		name                     string
		tenant, policy, artifact uuid.UUID
		sha                      string
	}{
		{"missing", tenant, policy, artifact, sha},
		{"foreign_tenant", uuid.New(), policy, artifact, sha},
		{"foreign_policy", tenant, uuid.New(), artifact, sha},
		{"foreign_artifact", tenant, policy, uuid.New(), sha},
		{"zero_policy", tenant, uuid.Nil, artifact, sha},
		{"bad_digest", tenant, policy, artifact, "invalid"},
	} {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_policy_ready($1,$2,$3,$4)`, test.tenant, test.policy, test.artifact, test.sha).Scan(&accepted); err != nil {
			t.Fatal(test.name, err)
		}
		if accepted {
			t.Fatal("invalid publication accepted", test.name)
		}
	}
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal"} {
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1,'photo.v31_signing_policy_ready(uuid,uuid,uuid,text)','EXECUTE')`, role).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Fatal("private helper exposed", role)
		}
	}
	var version *string
	if err := tx.QueryRow(ctx, `SELECT photo.v31_ready_signing_policy_version($1,$2,$3,$4,'{}'::jsonb,$5,$6,$7,$8,$9)`, tenant, uuid.New(), uuid.New(), uuid.New(), policy, artifact, sha, int64(10), "photos").Scan(&version); err != nil || version != nil {
		t.Fatal("unowned ready policy version exposed", err, version)
	}
	lineageArtifact := uuid.NewSHA1(tenant, []byte("signing:lineage:"+sha))
	if err := tx.QueryRow(ctx, `SELECT photo.v31_ready_signing_lineage_version($1,$2,$3,$4,'{}'::jsonb,$5,$6,$7,$8)`, tenant, uuid.New(), uuid.New(), tenant, lineageArtifact, sha, int64(10), "photos").Scan(&version); err != nil || version != nil {
		t.Fatal("unowned ready lineage version exposed", err, version)
	}
	var owner string
	var security bool
	if err := tx.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef FROM pg_proc p WHERE p.oid='photo.v31_ready_signing_policy_version(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text)'::regprocedure`).Scan(&owner, &security); err != nil || owner != "photo_fn_owner" || !security {
		t.Fatal("ready-version helper owner/definer changed", err, owner, security)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef FROM pg_proc p WHERE p.oid='photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)'::regprocedure`).Scan(&owner, &security); err != nil || owner != "photo_fn_owner" || !security {
		t.Fatal("ready-lineage helper owner/definer changed", err, owner, security)
	}
	for _, test := range []struct {
		signature, role string
		allowed         bool
	}{
		{"photo.reserve_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text)", "photo_api", true},
		{"photo.reserve_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text)", "photo_scheduler", false},
		{"photo.complete_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,uuid,uuid,text)", "photo_api", true},
		{"photo.complete_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,uuid,uuid,text)", "photo_worker", false},
		{"photo.v31_ready_signing_policy_version(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text)", "photo_api", true},
		{"photo.v31_ready_signing_policy_version(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text)", "photo_scheduler", false},
		{"photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)", "photo_api", true},
		{"photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)", "photo_scheduler", false},
	} {
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, test.role, test.signature).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != test.allowed {
			t.Fatal("unexpected privilege", test.role, test.signature)
		}
	}
}
