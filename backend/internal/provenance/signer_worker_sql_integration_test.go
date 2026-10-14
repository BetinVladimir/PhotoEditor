package provenance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Registration boundary assertions only; not SDK, dispatch or signing evidence.
// Requires isolated admin database migrated through 01399. Always rolls back.
func TestIntegrationSignerWorkerQualification(t *testing.T) {
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
	var user, tenant uuid.UUID
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@signer.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM photo.tenants WHERE owner_user_id=$1`, user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	worker := uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := tx.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	a, b, c, d := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
	exec(`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,enabled,db_principal,qualified_operations,execution_pool,signer_sdk_lock_sha,signer_certificate_chain_sha,signer_trust_bundle_sha,signer_qualification_sha,signer_qualified_until)
 VALUES($1,$2,'["cpu"]','["cpu"]','',clock_timestamp(),'{}',true,session_user,'{"provenance.sign":["cpu"]}','signer',$3,$4,$5,$6,clock_timestamp()+interval '1 hour')`, tenant, worker, a, b, c, d)
	profile := map[string]any{"execution_pool": "signer", "backend": "cpu", "renderer_version": "v2.2", "sdk_lock_sha": a, "certificate_chain_sha": b, "trust_bundle_sha": c}
	matches := func(tid uuid.UUID, operation string, p map[string]any) bool {
		t.Helper()
		raw, e := json.Marshal(p)
		if e != nil {
			t.Fatal(e)
		}
		var ok bool
		if e = tx.QueryRow(ctx, `SELECT photo.v31_signer_worker_matches($1,$2,$3,$4::jsonb)`, tid, worker, operation, string(raw)).Scan(&ok); e != nil {
			t.Fatal(e)
		}
		return ok
	}
	if !matches(tenant, "provenance.sign", profile) {
		t.Fatal("exact installed profile refused")
	}
	if matches(uuid.New(), "provenance.sign", profile) || matches(tenant, "metadata.write", profile) {
		t.Fatal("foreign tenant/general operation accepted")
	}
	for _, key := range []string{"sdk_lock_sha", "certificate_chain_sha", "trust_bundle_sha", "execution_pool", "backend", "renderer_version"} {
		changed := map[string]any{}
		for k, v := range profile {
			changed[k] = v
		}
		changed[key] = "wrong"
		if matches(tenant, "provenance.sign", changed) {
			t.Fatalf("substituted %s accepted", key)
		}
	}
	exec(`UPDATE photo.worker_registrations SET signer_qualified_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND worker_id=$2`, tenant, worker)
	if matches(tenant, "provenance.sign", profile) {
		t.Fatal("expired signer accepted")
	}
	exec(`UPDATE photo.worker_registrations SET signer_qualified_until=clock_timestamp()+interval '1 hour',enabled=false WHERE tenant_id=$1 AND worker_id=$2`, tenant, worker)
	if matches(tenant, "provenance.sign", profile) {
		t.Fatal("disabled signer accepted")
	}
	// The worker cannot turn a general registration into a signer merely by
	// advertising an operation. Invalid provisioning is a constraint violation.
	exec(`SAVEPOINT invalid_signer`)
	_, err = tx.Exec(ctx, `INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,qualified_operations) VALUES($1,$2,'["cpu"]','["cpu"]','',clock_timestamp(),'{}','{"provenance.sign":["cpu"]}')`, tenant, uuid.New())
	if err == nil {
		t.Fatal("unqualified general signer registration accepted")
	}
	exec(`ROLLBACK TO SAVEPOINT invalid_signer`)
	var missing int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM photo.v31_signing_adoption_params($1,$2,1,1,$3)`, tenant, uuid.New(), uuid.New()).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatal("missing canonical signer result admitted")
	}
	var committed bool
	if err = tx.QueryRow(ctx, `SELECT photo.adopt_v31_signed($1,$2,1,1,$3,'[]','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','{}','{}','{}','{}','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')`, tenant, uuid.New(), uuid.New()).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("missing signer task committed")
	}
	if err = tx.QueryRow(ctx, `SELECT photo.v31_signing_adoption_committed($1,$2)`, tenant, uuid.New()).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("missing signer commit acknowledged")
	}
	var adoptionExposed bool
	if err = tx.QueryRow(ctx, `SELECT has_function_privilege('photo_worker','photo.v31_signing_adoption_params(uuid,uuid,integer,bigint,uuid)','EXECUTE') OR has_function_privilege('photo_scheduler','photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)','EXECUTE') OR has_function_privilege('photo_scheduler','photo.adopt_v2_plan_pre_v31_signer(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')`).Scan(&adoptionExposed); err != nil {
		t.Fatal(err)
	}
	if adoptionExposed {
		t.Fatal("signer adoption/activation bypass exposed")
	}
	var exposed bool
	if err = tx.QueryRow(ctx, `SELECT has_function_privilege('photo_worker','photo.v31_signer_worker_matches(uuid,uuid,text,jsonb)','EXECUTE') OR has_function_privilege('photo_worker','photo.claim_task_pre_v31_signer(uuid,integer,uuid,jsonb)','EXECUTE') OR has_table_privilege('photo_worker','photo.worker_registrations','UPDATE')`).Scan(&exposed); err != nil {
		t.Fatal(err)
	}
	if exposed {
		t.Fatal("worker qualification/bypass authority exposed")
	}
}
