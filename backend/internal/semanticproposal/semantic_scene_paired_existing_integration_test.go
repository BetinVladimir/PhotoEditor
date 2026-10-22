package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

// Protocol fixture reports are synthetic; production publication, admission,
// dispatch, claim and heartbeat are used unchanged. No completed roots are seeded.
func TestIntegrationScenePairedExistingClaimContext(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_SCENE_PAIRED_CASES")
	if raw == "" {
		t.Skip("two isolated owned source fixture envelopes required")
	}
	var cases []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &cases); err != nil || len(cases) != 2 {
		t.Fatal("paired envelopes", err)
	}
	tenants := make([]uuid.UUID, 2)
	for i, body := range cases {
		var value struct{ Tenant uuid.UUID }
		if json.Unmarshal(body, &value) != nil || value.Tenant == uuid.Nil {
			t.Fatal("paired tenant")
		}
		tenants[i] = value.Tenant
		t.Setenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE", string(body))
		t.Setenv("PHOTO_TEST_V27_SCENE_WIRE", "1")
		t.Setenv("PHOTO_TEST_SCENE_PAIRED_PREPARE_ONLY", "1")
		// Reuse actual intent/snapshot/detector/scene publishers and admission.
		TestIntegrationNativePreparationBootstrap(t)
	}
	if tenants[0] == tenants[1] {
		t.Fatal("paired tenants must differ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var principal, role string
	if err = tx.QueryRow(ctx, "SELECT session_user,current_user").Scan(&principal, &role); err != nil || principal != role {
		t.Fatal("explicit fixture owner connection required", err)
	}
	type member struct {
		Tenant, Task, Worker uuid.UUID
		Node, Command        string
		Attempt              int
		Fence                int64
		RAM, VRAM, Scratch   int64
	}
	pair := make([]member, 2)
	setRole := func(value string) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+value); err != nil {
			t.Fatal(err)
		}
	}
	for i, tenant := range tenants {
		v := &pair[i]
		v.Tenant = tenant
		v.Worker = uuid.New()
		v.Node = "scene-paired-" + uuid.NewString()
		var profile []byte
		// Strict singleton selection: never choose an arbitrary historical child.
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=$1 AND t.operation='scene.parse' AND t.operation_version='1' AND t.state='ready' AND (r.kind='v2' AND r.graph_sealed)`, tenant).Scan(&count); err != nil || count != 1 {
			t.Fatal("exact paired ready source", i, count, err)
		}
		if err = tx.QueryRow(ctx, `SELECT t.id,t.execution_profile FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=$1 AND t.operation='scene.parse' AND t.operation_version='1' AND t.state='ready' AND (r.kind='v2' AND r.graph_sealed)`, tenant).Scan(&v.Task, &profile); err != nil {
			t.Fatal(err)
		}
		var p struct {
			Backend   string `json:"backend"`
			Lock      string `json:"model_lock_sha"`
			Resources struct {
				RAM     int64 `json:"ram_bytes"`
				VRAM    int64 `json:"vram_bytes"`
				Scratch int64 `json:"scratch_bytes"`
			} `json:"resource_estimate"`
		}
		if json.Unmarshal(profile, &p) != nil || (p.Backend != "cuda" && p.Backend != "mps") || len(p.Lock) != 64 {
			t.Fatal("actual closed scene profile")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,34359738368,34359738368,34359738368,34359738368,34359738368,34359738368,clock_timestamp(),$2)`, v.Node, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
		backends, _ := json.Marshal([]string{p.Backend})
		qualified, _ := json.Marshal(map[string][]string{"scene.parse": {p.Backend}})
		if _, err = tx.Exec(ctx, `INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions,certificate_sha256) VALUES($1,$2,'["gpu"]',$3,$4,clock_timestamp(),'{}',session_user,$5,true,$6,'["v2.2"]',repeat('9',64))`, tenant, v.Worker, backends, p.Lock, v.Node, qualified); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,'gpu1','gpu','{}')`, v.Node); err != nil {
			t.Fatal(err)
		}
		v.RAM, v.VRAM, v.Scratch = p.Resources.RAM, p.Resources.VRAM, p.Resources.Scratch
	}

	if pair[0].Task == pair[1].Task || pair[0].Worker == pair[1].Worker {
		t.Fatal("paired entities must be distinct")
	}
	const snapshot = `SELECT jsonb_build_object(
 'runs',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.id),'[]') FROM photo.runs r WHERE r.tenant_id=ANY($1::uuid[])),
 'results',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]') FROM photo.task_results r WHERE r.tenant_id=ANY($1::uuid[])),
 'inputs',(SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY to_jsonb(i)::text),'[]') FROM photo.v2_task_inputs i WHERE i.tenant_id=ANY($1::uuid[])),
 'artifacts',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM photo.artifacts a WHERE a.tenant_id=ANY($1::uuid[])),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY t.id) FROM photo.tasks t WHERE t.tenant_id=ANY($1::uuid[])),
 'attempts',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY to_jsonb(a)::text),'[]') FROM photo.task_attempts a WHERE a.tenant_id=ANY($1::uuid[])),
 'slots',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY to_jsonb(s)::text),'[]') FROM photo.resource_slots s WHERE s.node_id=ANY($2::text[])),
 'reservations',(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]') FROM photo.resource_reservations r WHERE r.tenant_id=ANY($1::uuid[])),
 'quota',(SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY to_jsonb(q)::text),'[]') FROM photo.quota_reservations q WHERE q.tenant_id=ANY($1::uuid[])),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY to_jsonb(o)::text),'[]') FROM photo.outbox o WHERE o.tenant_id=ANY($1::uuid[])),
 'workers',(SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY to_jsonb(w)::text),'[]') FROM photo.worker_registrations w WHERE w.tenant_id=ANY($1::uuid[])),
 'usage',(SELECT coalesce(jsonb_agg(to_jsonb(u) ORDER BY to_jsonb(u)::text),'[]') FROM photo.tenant_usage u WHERE u.tenant_id=ANY($1::uuid[])))::text`
	nodes := []string{pair[0].Node, pair[1].Node}
	readSnapshot := func() string {
		t.Helper()
		setRole("NONE")
		var value string
		if err := tx.QueryRow(ctx, snapshot, tenants, nodes).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	beforeReserve := readSnapshot()
	setRole("photo_scheduler")
	for i, v := range pair {
		var outcome string
		var command []byte
		other := pair[1-i]
		if err = tx.QueryRow(ctx, "SELECT * FROM photo.reserve_dispatch($1,$2,$3,$4,$5,$6,$7,$8)", v.Task, other.Node, v.RAM, v.VRAM, v.Scratch, uuid.New(), uuid.New(), "").Scan(&outcome, &command); err != nil || outcome != "waiting_worker" || command != nil {
			t.Fatal("existing foreign node reserve refusal", i, outcome, err)
		}
	}
	if readSnapshot() != beforeReserve {
		t.Fatal("foreign node reserve changed either source graph")
	}
	for i := range pair {
		v := &pair[i]
		setRole("photo_scheduler")
		var outcome string
		var cmd []byte
		if err = tx.QueryRow(ctx, "SELECT * FROM photo.reserve_dispatch($1,$2,$3,$4,$5,$6,$7,$8)", v.Task, v.Node, v.RAM, v.VRAM, v.Scratch, uuid.New(), uuid.New(), "").Scan(&outcome, &cmd); err != nil || outcome != "queued" {
			t.Fatal("actual paired dispatch", i, outcome, err)
		}
		v.Command = string(cmd)
		var command struct {
			Attempt int       `json:"attempt"`
			Tenant  uuid.UUID `json:"tenant_id"`
			Task    uuid.UUID `json:"task_id"`
		}
		if json.Unmarshal(cmd, &command) != nil || command.Attempt < 1 || command.Tenant != v.Tenant || command.Task != v.Task {
			t.Fatal("actual paired command identity")
		}
		v.Attempt = command.Attempt
		setRole("NONE")
	}

	before := readSnapshot()
	setRole("photo_worker")
	var guc string
	if err = tx.QueryRow(ctx, "SELECT coalesce(current_setting('app.tenant_id',true),'')").Scan(&guc); err != nil || guc != "" {
		t.Fatal("paired claim must have no API GUC", err)
	}
	for i, v := range pair {
		other := pair[1-i]
		var outcome string
		if err = tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$2,$3,$4::jsonb)", v.Task, v.Attempt, other.Worker, v.Command).Scan(&outcome); err != nil || outcome != "unauthorized" {
			t.Fatal("existing foreign worker refusal", i, outcome, err)
		}
		for _, field := range []string{"tenant_id", "task_id"} {
			var altered map[string]any
			if json.Unmarshal([]byte(v.Command), &altered) != nil {
				t.Fatal("command decode")
			}
			if field == "tenant_id" {
				altered[field] = other.Tenant.String()
			} else {
				altered[field] = other.Task.String()
			}
			body, err := json.Marshal(altered)
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$2,$3,$4::jsonb)", v.Task, v.Attempt, v.Worker, string(body)).Scan(&outcome); err != nil || outcome != "stale" {
				t.Fatal("existing foreign command identity refusal", i, field, outcome, err)
			}
		}
	}
	if after := readSnapshot(); after != before {
		t.Fatal("paired claim refusals changed either scoped graph")
	}
	setRole("photo_worker")
	for i := range pair {
		v := &pair[i]
		var outcome string
		var until, deadline time.Time
		var leaseContext []byte
		if err = tx.QueryRow(ctx, "SELECT * FROM photo.claim_task($1,$2,$3,$4::jsonb)", v.Task, v.Attempt, v.Worker, v.Command).Scan(&outcome, &v.Fence, &until, &deadline, &leaseContext); err != nil || outcome != "claimed" || v.Fence < 1 || until.After(deadline) {
			t.Fatal("legitimate paired own claim", i, outcome, err)
		}
	}
	before = readSnapshot()
	setRole("photo_worker")
	for i, v := range pair {
		other := pair[1-i]
		// Both workers/tasks exist and both fences came from successful claims.
		for _, args := range [][]any{{v.Task, v.Attempt, v.Fence, other.Worker}, {other.Task, other.Attempt, other.Fence, v.Worker}} {
			var accepted bool
			if err = tx.QueryRow(ctx, "SELECT accepted FROM photo.heartbeat_task($1,$2,$3,$4,'{}'::jsonb)", args...).Scan(&accepted); err != nil || accepted {
				t.Fatal("existing cross-tenant heartbeat refusal", i, err)
			}
		}
	}
	if after := readSnapshot(); after != before {
		t.Fatal("paired heartbeat refusals changed either scoped graph")
	}
	setRole("photo_worker")
	for i, v := range pair {
		var accepted bool
		if err = tx.QueryRow(ctx, "SELECT accepted FROM photo.heartbeat_task($1,$2,$3,$4,'{}'::jsonb)", v.Task, v.Attempt, v.Fence, v.Worker).Scan(&accepted); err != nil || !accepted {
			t.Fatal("legitimate paired own heartbeat", i, err)
		}
	}
	// Roll back only dispatch/lease probe state; actual source admissions remain
	// available to the owning disposable DB's normal runner cleanup.
	if err = tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
		t.Fatal(err)
	}
	t.Log("SCENE_PAIRED_EXISTING_CLAIM_CONTEXT_OK")
}
