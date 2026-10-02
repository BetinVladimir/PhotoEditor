package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationRecovery(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("scheduler database unavailable")
	}
	defer pool.Close()
	var user, tenant, params, snapshot, run uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if err = admin.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id", tenant, uuid.NewString(), hash).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenant, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'apply','running',$2,'{}',$3,$4,true) RETURNING id", tenant, snapshot, hash, user).Scan(&run); err != nil {
		t.Fatal(err)
	}
	task, worker := uuid.New(), uuid.New()
	_, err = admin.Exec(ctx, `INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,worker_id,lease_until,deadline)
 VALUES($1,$2,$3,'test','asset.verify','1','media','running','[]',$4,$5,'{"backend":"cpu"}',1,1,$6,now()-interval '1 second',now()+interval '600 seconds')`, task, tenant, run, params, hash, worker)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES($1,$2,1,1,$3,now()-interval '10 seconds','cpu','{"backend":"cpu"}')`, tenant, task, worker)
	if err != nil {
		t.Fatal(err)
	}
	r := Recovery{DB: pool}
	if _, err = r.Recover(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var state string
	var retries, attempt int
	var fence int64
	var next *time.Time
	if err = admin.QueryRow(ctx, "SELECT state,retry_count,attempt,fencing_token,next_attempt_at FROM photo.tasks WHERE id=$1", task).Scan(&state, &retries, &attempt, &fence, &next); err != nil {
		t.Fatal(err)
	}
	if state != "retry_wait" || retries != 1 || attempt != 1 || fence != 2 || next == nil {
		t.Fatal("invalid recovery", state, retries, attempt, fence, next)
	}
	if _, err = r.Recover(ctx, 100); err != nil {
		t.Fatal(err)
	}
	var stable int
	if err = admin.QueryRow(ctx, "SELECT retry_count FROM photo.tasks WHERE id=$1", task).Scan(&stable); err != nil || stable != 1 {
		t.Fatal("duplicate recovery consumed budget", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.tasks SET next_attempt_at=now()-interval '1 second' WHERE id=$1", task); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Recover(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "ready" {
		t.Fatal("retry not activated", state, err)
	}
}

func TestIntegrationDispatcherTriesNextNode(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("scheduler database unavailable")
	}
	defer pool.Close()
	var user, tenant, params, snapshot, run uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 64)
	if err = admin.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id", tenant, uuid.NewString(), hash).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenant, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'apply','running',$2,'{}',$3,$4,true) RETURNING id", tenant, snapshot, hash, user).Scan(&run); err != nil {
		t.Fatal(err)
	}
	task := uuid.New()
	_, err = admin.Exec(ctx, `INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES($1,$2,$3,'dispatch','asset.verify','1','media','ready','[]',$4,$5,'{"backend":"cpu","resource_estimate":{"ram_bytes":2048,"vram_bytes":0,"scratch_bytes":1024,"output_bytes":0,"pixel_count":0}}')`, task, tenant, run, params, hash)
	if err != nil {
		t.Fatal(err)
	}
	small, large := "a-small-"+uuid.NewString(), "z-large-"+uuid.NewString()
	for _, node := range []string{small, large} {
		capacity := int64(10000000)
		if node == small {
			capacity = 1024
		}
		_, err = admin.Exec(ctx, `INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,$2,0,10000000,$2,10000000,clock_timestamp(),$3)`, node, capacity, hash)
		if err != nil {
			t.Fatal(err)
		}
		_, err = admin.Exec(ctx, `INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,'media1','media','{}')`, node)
		if err != nil {
			t.Fatal(err)
		}
		_, err = admin.Exec(ctx, `INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations) VALUES($1,$2,'["media"]','["cpu"]',$3,clock_timestamp(),'{}',session_user,$4,true,'{"asset.verify":["cpu"]}')`, tenant, uuid.New(), hash, node)
		if err != nil {
			t.Fatal(err)
		}
	}
	dispatcher := Dispatcher{DB: pool}
	if n, e := dispatcher.Dispatch(ctx, 100); e != nil || n != 1 {
		t.Fatal("dispatch", n, e)
	}
	if n, e := dispatcher.Dispatch(ctx, 100); e != nil || n != 0 {
		t.Fatal("duplicate dispatch", n, e)
	}
	var assigned string
	var raw []byte
	if err = admin.QueryRow(ctx, "SELECT node_id FROM photo.resource_reservations WHERE task_id=$1 AND state='reserved'", task).Scan(&assigned); err != nil || assigned != large {
		t.Fatal("wrong resource selection", assigned, err)
	}
	if err = admin.QueryRow(ctx, "SELECT dispatch_command FROM photo.tasks WHERE id=$1", task).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var command map[string]any
	if json.Unmarshal(raw, &command) != nil || command["timeout_seconds"] != float64(300) {
		t.Fatal("invalid dispatch command")
	}
	var finish int64
	if err = admin.QueryRow(ctx, "SELECT virtual_finish FROM photo.scheduler_tenants WHERE tenant_id=$1", tenant).Scan(&finish); err != nil || finish != 1 {
		t.Fatal("fairness", finish, err)
	}
	var recorded bool
	if err = pool.QueryRow(ctx, "SELECT photo.record_dispatch($1,$2)", task, command["event_id"]).Scan(&recorded); err != nil || !recorded {
		t.Fatal("accounting replay", err)
	}
	if err = admin.QueryRow(ctx, "SELECT virtual_finish FROM photo.scheduler_tenants WHERE tenant_id=$1", tenant).Scan(&finish); err != nil || finish != 1 {
		t.Fatal("duplicate accounting", finish, err)
	}
}
