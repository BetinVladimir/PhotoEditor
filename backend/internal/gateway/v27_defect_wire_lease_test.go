package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The caller owns a disposable DB. Obtain a real reservation/claim; never patch
// a task into running or invent a fence for the wire operation.
func claimNativeDefectWireTask(t *testing.T, ctx context.Context, db *pgxpool.Pool, task uuid.UUID, certificate string) {
	claimNativeWireTask(t, ctx, db, task, certificate, "defects.propose", "cpu", "cpu")
}

func claimNativeWireTask(t *testing.T, ctx context.Context, db *pgxpool.Pool, task uuid.UUID, certificate, operation, class, backend string) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var tenant uuid.UUID
	var profile []byte
	if err := tx.QueryRow(ctx, "SELECT tenant_id,execution_profile FROM photo.tasks WHERE id=$1 AND state='ready' AND operation=$2", task, operation).Scan(&tenant, &profile); err != nil {
		t.Fatal("native wire ready task", err)
	}
	var estimate struct {
		Resources struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &estimate) != nil {
		t.Fatal("native profile")
	}
	node, worker := "native-wire-"+uuid.NewString(), uuid.New()
	if _, err := tx.Exec(ctx, "INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,34359738368,34359738368,34359738368,34359738368,34359738368,34359738368,clock_timestamp(),$2)", node, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	classes, _ := json.Marshal([]string{class})
	backends, _ := json.Marshal([]string{backend})
	qualified, _ := json.Marshal(map[string][]string{operation: {backend}})
	var identities map[string]any
	if err := json.Unmarshal(profile, &identities); err != nil {
		t.Fatal(err)
	}
	lock := strings.Repeat("a", 64)
	if value, ok := identities["model_lock_id"].(string); ok {
		lock = value
	} else if operation == "mask.person" || operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" || operation == "scene.parse" || operation == "depth.predict" || operation == "semantic.transfer" {
		if value, ok := identities["model_lock_sha"].(string); ok {
			lock = value
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions,certificate_sha256) VALUES($1,$2,$3,$4,$5,clock_timestamp(),'{}',session_user,$6,true,$7,'["v2.2"]',$8)`, tenant, worker, classes, backends, lock, node, qualified, certificate); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,$2,$3,'{}')", node, class+"1", class); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var outcome string
	var command []byte
	if err := tx.QueryRow(ctx, "SELECT * FROM photo.reserve_dispatch($1,$2,$3,$4,$5,$6,$7,$8)", task, node, estimate.Resources.RAM, estimate.Resources.VRAM, estimate.Resources.Scratch, uuid.New(), uuid.New(), "").Scan(&outcome, &command); err != nil || outcome != "queued" {
		t.Fatal("native wire dispatch", outcome, err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
		t.Fatal(err)
	}
	var beforeRejectedClaim string
	if err := tx.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM photo.tasks t WHERE id=$1", task).Scan(&beforeRejectedClaim); err != nil {
		t.Fatal(err)
	}
	var dispatched struct {
		Attempt int `json:"attempt"`
	}
	if err := json.Unmarshal(command, &dispatched); err != nil || dispatched.Attempt < 1 {
		t.Fatal("dispatch attempt", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
		t.Fatal(err)
	}
	var fence int64
	var until, deadline time.Time
	var leaseContext []byte
	if operation == "mask.person" || operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" || operation == "scene.parse" || operation == "depth.predict" {
		var altered map[string]any
		if json.Unmarshal(command, &altered) != nil {
			t.Fatal("person command")
		}
		altered["model"].(map[string]any)["lock_id"] = strings.Repeat("0", 64)
		invalid, err := json.Marshal(altered)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$4,$2,$3::jsonb)", task, worker, string(invalid), dispatched.Attempt).Scan(&outcome); err != nil || outcome != "unauthorized" {
			t.Fatal("person substituted command lock", outcome, err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE photo.worker_registrations SET lock_id=$1 WHERE tenant_id=$2 AND worker_id=$3", strings.Repeat("0", 64), tenant, worker); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$4,$2,$3::jsonb)", task, worker, string(command), dispatched.Attempt).Scan(&outcome); err != nil || outcome != "unauthorized" {
			t.Fatal("person wrong installed worker lock", outcome, err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE photo.worker_registrations SET lock_id=$1 WHERE tenant_id=$2 AND worker_id=$3", lock, tenant, worker); err != nil {
			t.Fatal(err)
		}
		var state string
		var afterRejectedClaim string
		if err := tx.QueryRow(ctx, "SELECT state,to_jsonb(t)::text FROM photo.tasks t WHERE id=$1", task).Scan(&state, &afterRejectedClaim); err != nil || state != "queued" || afterRejectedClaim != beforeRejectedClaim {
			t.Fatal("rejected person claim mutated task", state, err)
		}
		for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_gateway", "photo_auth", "photo_publisher", "photo_maintenance", "photo_portal"} {
			var canClaim, canReserve, canResources bool
			if err := tx.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.claim_task(uuid,integer,uuid,jsonb)','EXECUTE'),has_function_privilege($1,'photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)','EXECUTE'),has_function_privilege($1,'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)','EXECUTE')", role).Scan(&canClaim, &canReserve, &canResources); err != nil || canClaim != (role == "photo_worker") || canReserve != (role == "photo_scheduler") || canResources {
				t.Fatal("person dispatch role matrix", role, err)
			}
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
			t.Fatal(err)
		}
	}
	const sceneSnapshotQuery = `SELECT jsonb_build_object('task',to_jsonb(t),
     'slots',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY to_jsonb(s)::text),'[]') FROM photo.resource_slots s WHERE s.task_id=t.id),
     'reservations',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]') FROM photo.resource_reservations r WHERE r.task_id=t.id),
     'quota',(SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY to_jsonb(q)::text),'[]') FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id AND q.holder_id=t.run_id),
     'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY to_jsonb(o)::text),'[]') FROM photo.outbox o WHERE o.tenant_id=t.tenant_id),
     'workers',(SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY to_jsonb(w)::text),'[]') FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id),
     'usage',(SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE u.tenant_id=t.tenant_id))::text FROM photo.tasks t WHERE t.id=$1`
	if operation == "scene.parse" || operation == "depth.predict" {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
			t.Fatal(err)
		}
		var beforeScope, afterScope string
		if err := tx.QueryRow(ctx, sceneSnapshotQuery, task).Scan(&beforeScope); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
			t.Fatal(err)
		}
		var guc string
		if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('app.tenant_id',true),'')").Scan(&guc); err != nil || guc != "" {
			t.Fatal("scene claim must exercise absent API tenant context", err)
		}
		if err := tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$4,$2,$3::jsonb)", task, uuid.New(), string(command), dispatched.Attempt).Scan(&outcome); err != nil || outcome != "unauthorized" {
			t.Fatal("scene foreign worker claim", outcome, err)
		}
		// Wrong tenant/task in the command must be refused before a lease exists.
		for _, field := range []string{"tenant_id", "task_id"} {
			var altered map[string]any
			if err := json.Unmarshal(command, &altered); err != nil {
				t.Fatal(err)
			}
			altered[field] = uuid.NewString()
			invalid, err := json.Marshal(altered)
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$4,$2,$3::jsonb)", task, worker, string(invalid), dispatched.Attempt).Scan(&outcome); err != nil || outcome != "stale" {
				t.Fatal("scene substituted command identity", field, outcome, err)
			}
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, sceneSnapshotQuery, task).Scan(&afterScope); err != nil || afterScope != beforeScope {
			t.Fatal("scene refused claim changed scoped state", err)
		}
		var unchanged string
		if err := tx.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM photo.tasks t WHERE id=$1", task).Scan(&unchanged); err != nil || unchanged != beforeRejectedClaim {
			t.Fatal("scene refused claim wrote task", err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
			t.Fatal(err)
		}
	}
	var semanticStatus string
	var semanticRevision int64
	if operation == "semantic.transfer" {
		if _,err:=tx.Exec(ctx,"SET LOCAL ROLE NONE");err!=nil {t.Fatal(err)}
		if err:=tx.QueryRow(ctx,"SELECT r.status,r.revision FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.id=$1",task).Scan(&semanticStatus,&semanticRevision);err!=nil {t.Fatal(err)}
		t.Log("SEMANTIC_TRANSFER_BEFORE_CLAIM",semanticStatus,semanticRevision)
		assertSemanticTransferRejectedClaims(t, ctx, tx, task, worker, dispatched.Attempt, command)
	}
	if err := tx.QueryRow(ctx, "SELECT * FROM photo.claim_task($1,$4,$2,$3::jsonb)", task, worker, string(command), dispatched.Attempt).Scan(&outcome, &fence, &until, &deadline, &leaseContext); err != nil || outcome != "claimed" || fence < 1 || until.After(deadline) {
		t.Fatal("native wire claim", outcome, err)
	}
	if operation == "semantic.transfer" {
		assertSemanticTransferClaimStarted(t, ctx, tx, task, worker, dispatched.Attempt, fence, semanticStatus, semanticRevision)
	}
	if operation == "scene.parse" || operation == "depth.predict" {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
			t.Fatal(err)
		}
		var before, after string
		if err := tx.QueryRow(ctx, sceneSnapshotQuery, task).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
			t.Fatal(err)
		}
		for _, foreignWorker := range []bool{false, true} {
			workerArg, fenceArg := worker, fence+1
			if foreignWorker {
				workerArg = uuid.New()
				fenceArg = fence
			}
			var accepted bool
			if err := tx.QueryRow(ctx, "SELECT accepted FROM photo.heartbeat_task($1,$2,$3,$4,'{}'::jsonb)", task, dispatched.Attempt, fenceArg, workerArg).Scan(&accepted); err != nil || accepted {
				t.Fatal("scene foreign worker/fence heartbeat", foreignWorker, err)
			}
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, sceneSnapshotQuery, task).Scan(&after); err != nil || after != before {
			t.Fatal("scene rejected heartbeat wrote lease/accounting", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
