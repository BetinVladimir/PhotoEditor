package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationCancelIdleTasks(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration73 required")
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
	ids := map[string]uuid.UUID{}
	for _, state := range []string{"blocked", "ready", "queued", "retry_wait", "running", "succeeded"} {
		id := uuid.New()
		ids[state] = id
		insertState := state
		if state == "succeeded" {
			insertState = "ready"
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,fencing_token,next_attempt_at,lease_until)
	VALUES($1,$2,$3,$4,'asset.verify','1','media',$7,'[]',$5,$6,'{"backend":"cpu"}',7,CASE WHEN $7='retry_wait' THEN now()+interval '1 hour' ELSE NULL END,CASE WHEN $7='running' THEN now()+interval '1 hour' ELSE NULL END)`, id, tenant, run, state, params, hash, insertState)
		if err != nil {
			t.Fatal(err)
		}
		if state == "succeeded" {
			var result uuid.UUID
			if _, err = tx.Exec(ctx, "INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,7,$3,now(),now(),'cpu','{}')", tenant, id, uuid.New()); err != nil {
				t.Fatal(err)
			}
			if err = tx.QueryRow(ctx, "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,7,'[]','{}',$3) RETURNING id", tenant, id, hash).Scan(&result); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET state='succeeded',canonical_result_id=$2 WHERE id=$1", id, result); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Task-local cancellation works even before the run itself is cancelled.
	if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=true WHERE id=$1", ids["blocked"]); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = tx.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&n); err != nil || n != 1 {
		t.Fatal("local cancellation", n, err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.runs SET cancel_requested=true,status='cancelling' WHERE id=$1", run); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = tx.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&n); err != nil || n != 1 {
			t.Fatal("run cancellation", n, err)
		}
	}
	if err = tx.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&n); err != nil || n != 0 {
		t.Fatal("replay cancellation", n, err)
	}
	for before, id := range ids {
		var state string
		var fence int64
		var next *time.Time
		if err = tx.QueryRow(ctx, "SELECT state,fencing_token,next_attempt_at FROM photo.tasks WHERE id=$1", id).Scan(&state, &fence, &next); err != nil {
			t.Fatal(err)
		}
		if before == "running" || before == "succeeded" {
			if state != before || fence != 7 {
				t.Fatal("active/terminal task mutated", before, state, fence)
			}
		} else if state != "cancelled" || fence != 8 || next != nil {
			t.Fatal("idle cancellation not fenced", before, state, fence, next)
		}
	}
	if err = tx.QueryRow(ctx, "SELECT photo.finalize_run_cancellation()").Scan(&n); err != nil {
		t.Fatal("running task must hold finalization", n, err)
	}
	var currentStatus string
	if err = tx.QueryRow(ctx, "SELECT status FROM photo.runs WHERE id=$1", run).Scan(&currentStatus); err != nil || currentStatus != "cancelling" {
		t.Fatal("running task was finalized", currentStatus, err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.tasks SET state='cancelled',cancel_requested=true,lease_until=NULL,fencing_token=fencing_token+1,terminal_reason='cancel_requested' WHERE id=$1", ids["running"]); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		if err = tx.QueryRow(ctx, "SELECT photo.finalize_run_cancellation()").Scan(&n); err != nil {
			t.Fatal("finalize cancellation", n, err)
		}
		if err = tx.QueryRow(ctx, "SELECT status FROM photo.runs WHERE id=$1", run).Scan(&currentStatus); err != nil {
			t.Fatal(err)
		}
		if currentStatus == "cancelled" {
			break
		}
		if n == 0 {
			t.Fatal("target run was not eligible for finalization")
		}
	}
	if currentStatus != "cancelled" {
		t.Fatal("target run was not finalized after bounded retries")
	}
	var runState, eventType string
	var revision int64
	if err = tx.QueryRow(ctx, "SELECT status,revision FROM photo.runs WHERE id=$1", run).Scan(&runState, &revision); err != nil || runState != "cancelled" || revision != 2 {
		t.Fatal("run not finalized", runState, revision, err)
	}
	if err = tx.QueryRow(ctx, "SELECT type FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2", tenant, run).Scan(&eventType); err != nil || eventType != "run.cancelled" {
		t.Fatal("cancellation event", eventType, err)
	}
	if err = tx.QueryRow(ctx, "SELECT photo.finalize_run_cancellation()").Scan(&n); err != nil {
		t.Fatal("finalization replay", n, err)
	}
	if err = tx.QueryRow(ctx, "SELECT status FROM photo.runs WHERE id=$1", run).Scan(&currentStatus); err != nil || currentStatus != "cancelled" {
		t.Fatal("finalization replay changed target", currentStatus, err)
	}
}
