package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

// Uses a rollback-only transaction in the isolated migration test database.
// In particular SQL NULL must not turn skipped-without-reason into success.
func TestIntegrationActivateBoundTask(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration47 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var user, tenant, params, snapshot, run uuid.UUID
	var epoch int64
	if err = tx.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if err = tx.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id", tenant, uuid.NewString(), hash).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenant, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'apply','running',$2,'{}',$3,$4,true) RETURNING id", tenant, snapshot, hash, user).Scan(&run); err != nil {
		t.Fatal(err)
	}
	parent, child := uuid.New(), uuid.New()
	for i, id := range []uuid.UUID{parent, child} {
		state := "blocked"
		if i == 0 {
			state = "skipped"
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES($1,$2,$3,$4,'asset.verify','1','media',$5,'[]',$6,$7,'{"backend":"cpu"}')`, id, tenant, run, id.String(), state, params, hash)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES($1,$2,$3,$4,'success')", tenant, run, parent, child); err != nil {
		t.Fatal(err)
	}
	activate := func(digest string, want bool) {
		t.Helper()
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
			t.Fatal(err)
		}
		ok, e := (AdvancementRepository{DB: tx}).Activate(ctx, BoundTaskActivation{TaskID: child, ParamsID: params, InputDigest: digest})
		if e != nil || ok != want {
			t.Fatalf("activation got %v want %v: %v", ok, want, e)
		}
		if _, err = tx.Exec(ctx, "RESET ROLE"); err != nil {
			t.Fatal(err)
		}
	}
	activate(hash, false)
	if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET terminal_reason='upstream_failed' WHERE id=$1", parent); err != nil {
		t.Fatal(err)
	}
	activate(hash, false)
	if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET terminal_reason='disabled' WHERE id=$1", parent); err != nil {
		t.Fatal(err)
	}
	activate(strings.Repeat("b", 64), false)
	if _, err = tx.Exec(ctx, "UPDATE photo.runs SET graph_sealed=false WHERE id=$1", run); err != nil {
		t.Fatal(err)
	}
	activate(hash, false)
	if _, err = tx.Exec(ctx, "UPDATE photo.runs SET graph_sealed=true WHERE id=$1", run); err != nil {
		t.Fatal(err)
	}
	activate(hash, true)
	activate(hash, true)
	var state string
	if err = tx.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", child).Scan(&state); err != nil || state != "ready" {
		t.Fatal("task not ready", err)
	}
}
