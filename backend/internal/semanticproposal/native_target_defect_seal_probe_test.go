package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func probeNativeTargetDefectSeal(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, built v2input.Built, owner *pgxpool.Pool) {
	t.Helper()
	seal := func(rollback bool) (v2input.JobTicket, error) {
		var ticket v2input.JobTicket
		sentinel := errors.New("rollback target defect seal fixture")
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				var err error
				ticket, err = p.Reader.sealNativeTargetDefectInTransaction(work, tx, who, original, plan, dependencies, built)
				if err != nil {
					return err
				}
				if rollback {
					return sentinel
				}
				return nil
			})
		})
		if rollback && errors.Is(err, sentinel) {
			return ticket, nil
		}
		if err == nil && !ok {
			t.Fatal("target defect seal parent lock missing")
		}
		return ticket, err
	}
	assertState := func(sealed bool) {
		var status, state string
		var actual bool
		var queued, events, receipts int
		err := owner.QueryRow(ctx, `SELECT r.status,r.graph_sealed,t.state,
   (SELECT count(*) FROM photo.outbox WHERE tenant_id=r.tenant_id AND payload->>'run_id'=r.id::text AND routing_key='v2.run.queued'),
   (SELECT count(*) FROM photo.run_events WHERE tenant_id=r.tenant_id AND run_id=r.id AND type='v2.run.queued'),
   (SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=r.tenant_id AND resource_id=r.id AND route='V2.Admit' AND response_status=202)
   FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2`, plan.shell.Tenant, plan.shell.Run).Scan(&status, &actual, &state, &queued, &events, &receipts)
		expected := 0
		wantStatus, wantState := "draft", "blocked"
		if sealed {
			expected = 1
			wantStatus, wantState = "queued", "ready"
		}
		if err != nil || actual != sealed || status != wantStatus || state != wantState || queued != expected || events != expected || receipts != expected {
			t.Fatal("target defect seal atomic state", status, actual, state, queued, events, receipts, err)
		}
	}
	if _, err := seal(true); err != nil {
		t.Fatal("target defect rollback seal", err)
	}
	assertState(false)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	_, revoked := seal(false)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revoked == nil {
		t.Fatal("target defect revoked seal accepted")
	}
	assertState(false)
	first, err := seal(false)
	if err != nil {
		t.Fatal("target defect seal", err)
	}
	again, err := seal(false)
	if err != nil || first != again {
		t.Fatal("target defect seal replay", err)
	}
	assertState(true)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	_, revoked = seal(false)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revoked == nil {
		t.Fatal("target defect revoked sealed replay accepted")
	}
	if _, err := seal(false); err != nil {
		t.Fatal("target defect restored sealed replay", err)
	}
	assertState(true)
	if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_EXECUTE") == "1" {
		probeNativeWireTaskOutputs(t, ctx, plan.shell.Tasks[0].ID)
		var status, state string
		var result string
		if err := owner.QueryRow(ctx, "SELECT r.status,t.state,t.canonical_result_id::text FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&status, &state, &result); err != nil || status != "running" || state != "succeeded" || result == "" {
			t.Fatal("target defect native execution", status, state, result, err)
		}
		if _, err := seal(false); err != nil {
			t.Fatal("target defect post execution sealed replay", err)
		}
		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_FINISH") == "1" {
			var id uuid.UUID
			var body []byte
			if err := owner.QueryRow(ctx, "SELECT id,outputs FROM photo.task_results WHERE tenant_id=$1 AND id=$2", plan.shell.Tenant, result).Scan(&id, &body); err != nil {
				t.Fatal(err)
			}
			var outputs []v2input.Ref
			if err := json.Unmarshal(body, &outputs); err != nil {
				t.Fatal(err)
			}
			finish := func(resultID uuid.UUID, refs []v2input.Ref) error {
				ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
					return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
						return p.Reader.finishNativeTargetDefectInTransaction(work, tx, who, original, plan, dependencies, built, resultID, refs)
					})
				})
				if err == nil && !ok {
					t.Fatal("target defect finish parent lock missing")
				}
				return err
			}
			if err := finish(uuid.New(), outputs); err == nil {
				t.Fatal("target defect foreign result finish accepted")
			}
			changed := append([]v2input.Ref(nil), outputs...)
			changed[0], changed[1] = changed[1], changed[0]
			if err := finish(id, changed); err == nil {
				t.Fatal("target defect reordered output finish accepted")
			}
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			revoked := finish(id, outputs)
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			if revoked == nil {
				t.Fatal("target defect revoked finish accepted")
			}
			if err := finish(id, outputs); err != nil {
				t.Fatal("target defect finish", err)
			}
			if err := finish(id, outputs); err != nil {
				t.Fatal("target defect finish replay", err)
			}
			var status, quota string
			var bytes int64
			var events, queued int
			if err := owner.QueryRow(ctx, `SELECT r.status,q.state,q.bytes,
			 (SELECT count(*) FROM photo.run_events WHERE tenant_id=r.tenant_id AND run_id=r.id AND type='v2.run.completed'),
			 (SELECT count(*) FROM photo.outbox WHERE tenant_id=r.tenant_id AND payload->>'run_id'=r.id::text AND routing_key='v2.run.completed')
			 FROM photo.runs r JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_id=r.id AND q.holder_type='run' WHERE r.tenant_id=$1 AND r.id=$2`, plan.shell.Tenant, plan.shell.Run).Scan(&status, &quota, &bytes, &events, &queued); err != nil || status != "completed" || quota != "released" || bytes != 0 || events != 1 || queued != 1 {
				t.Fatal("target defect finish exact once", status, quota, bytes, events, queued, err)
			}
			if _, err := seal(false); err != nil {
				t.Fatal("target defect completed seal replay", err)
			}
			if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_CONTENT") == "1" {
				completed, err := p.Reader.readCompletedNativeTargetDefect(ctx, who, plan, p.Configuration.Color)
				if err != nil {
					t.Fatal("target defect physical result", err)
				}
				repeated, err := p.Reader.readCompletedNativeTargetDefect(ctx, who, plan, p.Configuration.Color)
				if err != nil || !reflect.DeepEqual(completed, repeated) {
					t.Fatal("target defect physical result replay", err)
				}
				if completed.catalog.ResultID != id || !reflect.DeepEqual(completed.catalog.Outputs, outputs) {
					t.Fatal("target defect physical canonical identity")
				}
				if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
					t.Fatal(err)
				}
				_, revoked := p.Reader.readCompletedNativeTargetDefect(ctx, who, plan, p.Configuration.Color)
				if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
					t.Fatal(err)
				}
				if revoked == nil {
					t.Fatal("target defect revoked physical catalog accepted")
				}
				if _, err := p.Reader.readCompletedNativeTargetDefect(ctx, who, plan, p.Configuration.Color); err != nil {
					t.Fatal("target defect restored physical catalog", err)
				}
				if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_CONSUMER") == "1" {
					probeNativeTargetDefectConsumer(t, ctx, p, who, original, plan, dependencies, built, completed, owner)
				}
				if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_RECOVERY") == "1" {
					probeNativeTargetDefectRecovery(t, ctx, p, who, original, plan, built, owner)
				}
				t.Log("V27_TARGET_DEFECT_PHYSICAL_CONTENT_REPLAY_REVOKE_OK")
			}
			t.Log("V27_TARGET_DEFECT_FINISH_CANONICAL_REPLAY_REVOKE_QUOTA_OK")
		}
		t.Log("V27_TARGET_DEFECT_NATIVE_EXECUTION_CANONICAL_REPLAY_OK")
	}
	t.Log("V27_TARGET_DEFECT_ATOMIC_SEAL_ROLLBACK_REPLAY_REVOKE_OK")
}
