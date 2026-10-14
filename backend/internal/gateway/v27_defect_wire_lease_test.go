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
	} else if operation == "mask.person" || operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" {
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
	if operation == "mask.person" || operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" {
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
	if err := tx.QueryRow(ctx, "SELECT * FROM photo.claim_task($1,$4,$2,$3::jsonb)", task, worker, string(command), dispatched.Attempt).Scan(&outcome, &fence, &until, &deadline, &leaseContext); err != nil || outcome != "claimed" || fence < 1 || until.After(deadline) {
		t.Fatal("native wire claim", outcome, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
