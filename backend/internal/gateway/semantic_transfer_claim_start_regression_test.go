package gateway

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

// Runs only within claimNativeWireTask's genuine reserved task and real worker.
// Every owner mutation succeeds outside the expected-refusal call, is observed,
// and is rolled back before the unmodified positive claim. No lease is invented.
func assertSemanticTransferRejectedClaims(t *testing.T, ctx context.Context, tx pgx.Tx, task, worker uuid.UUID, attempt int, command []byte) {
	t.Helper()
	cases := []struct {
		name, mutation, changed   string
		wrongAttempt, wrongWorker bool
	}{
		{"expired-slot", "UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=$1", "SELECT EXISTS(SELECT FROM photo.resource_slots WHERE task_id=$1 AND lease_until<clock_timestamp())", false, false},
		{"task-cancel", "UPDATE photo.tasks SET cancel_requested=true WHERE id=$1", "SELECT cancel_requested FROM photo.tasks WHERE id=$1", false, false},
		{"run-cancel", "UPDATE photo.runs SET cancel_requested=true WHERE id=(SELECT run_id FROM photo.tasks WHERE id=$1)", "SELECT r.cancel_requested FROM photo.runs r JOIN photo.tasks t ON t.run_id=r.id WHERE t.id=$1", false, false},
		{"worker-disabled", "UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=$2 AND tenant_id=(SELECT tenant_id FROM photo.tasks WHERE id=$1)", "SELECT NOT w.enabled FROM photo.worker_registrations w JOIN photo.tasks t ON w.worker_id=$2 AND t.tenant_id=w.tenant_id WHERE t.id=$1", false, false},
		{"worker-unqualified", "UPDATE photo.worker_registrations SET qualified_operations='{}'::jsonb WHERE worker_id=$2 AND tenant_id=(SELECT tenant_id FROM photo.tasks WHERE id=$1)", "SELECT w.qualified_operations='{}'::jsonb FROM photo.worker_registrations w JOIN photo.tasks t ON w.worker_id=$2 AND t.tenant_id=w.tenant_id WHERE t.id=$1", false, false},
		{name: "wrong-attempt", wrongAttempt: true}, {name: "wrong-worker", wrongWorker: true},
	}
	for _, c := range cases {
		t.Run("semantic-transfer-claim/"+c.name, func(t *testing.T) {
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := tx.QueryRow(ctx, semanticClaimSnapshot, task, worker).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "SAVEPOINT semantic_claim_hostile"); err != nil {
				t.Fatal(err)
			}
			// Prove the same fixture is a genuine positive before every hostile.
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
				t.Fatal(err)
			}
			var baseline string
			if err := tx.QueryRow(ctx, "SELECT outcome FROM photo.claim_task($1,$2,$3,$4::jsonb)", task, attempt, worker, string(command)).Scan(&baseline); err != nil || baseline != "claimed" {
				t.Fatal("positive before hostile", c.name, baseline, err)
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			var running bool
			if err := tx.QueryRow(ctx, "SELECT r.status='running' AND t.state='running' FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=$1", task).Scan(&running); err != nil || !running {
				t.Fatal("positive claim run state", c.name, err)
			}
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT semantic_claim_hostile"); err != nil {
				t.Fatal(err)
			}
			if c.mutation != "" {
				args := []any{task}
				if c.name == "worker-disabled" || c.name == "worker-unqualified" {
					args = append(args, worker)
				}
				tag, err := tx.Exec(ctx, c.mutation, args...)
				if err != nil || tag.RowsAffected() != 1 {
					t.Fatal("hostile setup must succeed", c.name, tag, err)
				}
				var changed bool
				if err := tx.QueryRow(ctx, c.changed, args...).Scan(&changed); err != nil || !changed {
					t.Fatal("hostile setup not observed", c.name, err)
				}
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
				t.Fatal(err)
			}
			a, w := attempt, worker
			if c.wrongAttempt {
				a++
			}
			if c.wrongWorker {
				w = uuid.New()
			}
			// Compare the mutated fixture before rollback too: refusal must not write.
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			var mutated string
			if err := tx.QueryRow(ctx, semanticClaimSnapshot, task, worker).Scan(&mutated); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
				t.Fatal(err)
			}
			var outcome string
			var rejectedFence *int64
			var rejectedUntil, rejectedDeadline *time.Time
			var rejectedContext []byte
			err := tx.QueryRow(ctx, "SELECT * FROM photo.claim_task($1,$2,$3,$4::jsonb)", task, a, w, string(command)).Scan(&outcome, &rejectedFence, &rejectedUntil, &rejectedDeadline, &rejectedContext)
			if err != nil {
				var failure *pgconn.PgError
				if !errors.As(err, &failure) || failure.Code != "40001" || (failure.Message != "SEMANTIC_TRANSFER_CLAIM_START_STALE" && failure.Message != "V2_OPERATION_CLAIM_ACTIVATION_STALE") {
					t.Fatal("unexpected claim failure", c.name, err)
				}
			} else {
				allowed := false
				switch c.name {
				case "expired-slot":
					allowed = outcome == "unavailable"
				case "task-cancel", "run-cancel":
					allowed = outcome == "cancelled" || outcome == "stale"
				case "worker-disabled", "wrong-worker":
					allowed = outcome == "unauthorized"
				case "worker-unqualified":
					allowed = outcome == "unavailable" || outcome == "unauthorized"
				case "wrong-attempt":
					allowed = outcome == "stale"
				}
				if !allowed || rejectedFence != nil || rejectedUntil != nil || rejectedDeadline != nil || rejectedContext != nil {
					t.Fatal("unexpected refusal tuple", c.name, outcome)
				}
				if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
					t.Fatal(err)
				}
				var refused string
				if err := tx.QueryRow(ctx, semanticClaimSnapshot, task, worker).Scan(&refused); err != nil || refused != mutated {
					t.Fatal("refused claim mutated fixture", c.name, err)
				}
			}
			if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT semantic_claim_hostile"); err != nil {
				t.Fatal(err)
			}
			var after string
			if err := tx.QueryRow(ctx, semanticClaimSnapshot, task, worker).Scan(&after); err != nil || after != before {
				t.Fatal("hostile rollback changed fixture", c.name, err)
			}
			if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT semantic_claim_hostile"); err != nil {
				t.Fatal(err)
			}
		})
	}
	if t.Failed() {
		t.FailNow()
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
		t.Fatal(err)
	}
}

const semanticClaimSnapshot = `SELECT jsonb_build_object('task',to_jsonb(t),'run',to_jsonb(r),'slots',(SELECT jsonb_agg(to_jsonb(s) ORDER BY s.id) FROM photo.resource_slots s WHERE s.task_id=t.id),'workers',(SELECT jsonb_agg(to_jsonb(w) ORDER BY w.worker_id) FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=$2))::text FROM photo.tasks t JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id WHERE t.id=$1`

func assertSemanticTransferClaimStarted(t *testing.T, ctx context.Context, tx pgx.Tx, task, worker uuid.UUID, attempt int, fence int64, beforeStatus string, beforeRevision int64) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT r.kind='v2' AND r.status='running' AND NOT r.cancel_requested AND r.graph_sealed AND t.state='running' AND NOT t.cancel_requested AND t.attempt=$2 AND t.fencing_token=$3 AND t.worker_id=$4 FROM photo.tasks t JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id WHERE t.id=$1`, task, attempt, fence, worker).Scan(&ok); err != nil || !ok {
		t.Fatal("genuine semantic claim did not start run", err)
	}
	var revision int64
	if err := tx.QueryRow(ctx, "SELECT r.revision FROM photo.runs r JOIN photo.tasks t ON t.run_id=r.id AND t.tenant_id=r.tenant_id WHERE t.id=$1", task).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	expected := beforeRevision
	if beforeStatus == "queued" {
		expected++
	} else if beforeStatus != "running" {
		t.Fatal("unexpected genuine prior run state", beforeStatus)
	}
	if revision != expected {
		t.Fatal("run start revision must advance once", beforeStatus, beforeRevision, revision)
	}
	t.Log("SEMANTIC_TRANSFER_GENUINE_CLAIM_RUNNING", beforeStatus, beforeRevision, revision)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
		t.Fatal(err)
	}
}
