package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

// Rollback-only probe against the fixture's actual restricted admission role.
func assertAdmissionReceiptIsolation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, actor uuid.UUID) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	key := "isolation-" + uuid.NewString()
	insert := `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,$3,$4,$5,clock_timestamp()+interval '1 day')`
	if _, err = tx.Exec(ctx, insert, tenant, actor, ApplyIdempotencyRoute, key, strings.Repeat("a", 64)); err != nil {
		t.Fatal("scoped admission receipt insert", err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.idempotency_records SET response_status=200,response_body='{}'::jsonb WHERE tenant_id=$1 AND key=$2", tenant, key); err != nil {
		t.Fatal("response update denied", err)
	}
	deny := func(query string, args ...any) {
		t.Helper()
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = savepoint.Exec(ctx, query, args...)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
			savepoint.Rollback(ctx)
			t.Fatal("admission isolation did not deny operation", err)
		}
		if err = savepoint.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	deny(insert, tenant, actor, "Other.Route", uuid.NewString(), strings.Repeat("a", 64))
	deny(insert, uuid.New(), actor, ApplyIdempotencyRoute, uuid.NewString(), strings.Repeat("a", 64))
	deny("UPDATE photo.idempotency_records SET request_hash=$1 WHERE tenant_id=$2 AND key=$3", strings.Repeat("b", 64), tenant, key)
	deny("DELETE FROM photo.idempotency_records WHERE tenant_id=$1 AND key=$2", tenant, key)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE key=$1", key).Scan(&count); err != nil || count != 0 {
		t.Fatal("foreign tenant receipt exposed", err)
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id','',true)"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE key=$1", key).Scan(&count); err != nil || count != 0 {
		t.Fatal("unset tenant receipt exposed", err)
	}
}

// DDL and role switch stay inside a rollback-only owner transaction. The actual
// guard observes that transaction's metadata; other connections are unaffected.
func assertAdmissionRejectsDisabledRLS(t *testing.T, ctx context.Context, owner *pgxpool.Pool) {
	t.Helper()
	for _, ddl := range []string{"ALTER TABLE photo.idempotency_records DISABLE ROW LEVEL SECURITY", "ALTER TABLE photo.idempotency_records NO FORCE ROW LEVEL SECURITY"} {
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, ddl); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_semantic_admission"); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		err = verifyExposureAdmissionRole(ctx, tx)
		if !errors.Is(err, ErrUnavailable) {
			tx.Rollback(ctx)
			t.Fatal("unsafe receipt RLS accepted", err)
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
