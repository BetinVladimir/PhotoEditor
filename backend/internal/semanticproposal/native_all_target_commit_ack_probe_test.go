package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

var errNativeSealCommitAckLost = errors.New("injected acknowledgement loss after real seal COMMIT")

type nativeSealCommitAckLossTx struct {
	pgx.Tx
	committed *bool
}

func (tx nativeSealCommitAckLossTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	*tx.committed = true
	return errNativeSealCommitAckLost
}
func nativeSealCommitAckEnabled() bool {
	return os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_COMMIT_ACK") == "1"
}

// Exercise the same physical checks, SQL recovery callback and commit-aware
// ticket boundary used by resume. Only the acknowledgement is replaced.
func probeNativeSealCommitAckLoss(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, capacity TransferPreparation, built v2input.Built) {
	t.Helper()
	if err := p.Reader.recheckNativeAllTargetTransferPhysical(ctx, who, original.request, plan, capacity); err != nil {
		t.Fatal(err)
	}
	preparation := plan.reference.person.source.protection.published.preparation
	completed, err := p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, preparation.plan, capacity.Color, preparation.result, built)
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	begins := 0
	begin := func(ctx context.Context) (pgx.Tx, error) {
		begins++
		tx, err := p.Scheduler.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return nativeSealCommitAckLossTx{Tx: tx, committed: &committed}, nil
	}
	ticket, err := nativeTicketWithRetry(ctx, begin, func(tx pgx.Tx) (v2input.JobTicket, error) {
		return p.Reader.recoverNativeAllTargetTransferInTransaction(ctx, tx, who, original.request, original.requestKey, original.published, plan, capacity, completed)
	})
	if !errors.Is(err, errNativeSealCommitAckLost) || ticket != (v2input.JobTicket{}) || !committed || begins != 1 {
		t.Fatal("unknown seal commit exposed a ticket or retried", ticket, committed, begins, err)
	}
	t.Log("V27_NATIVE_ALL_TARGET_SEALED_COMMIT_ACK_LOST_OK")
}

// An independent owner connection observes the durable result before job replay.
// Compare complete parent state so a second activation cannot hide behind a
// matching public receipt or the final event-count assertion.
func observeNativeCommittedSeal(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) string {
	t.Helper()
	var valid bool
	err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   JOIN photo.idempotency_records i ON i.tenant_id=r.tenant_id AND i.resource_id=r.id
   WHERE r.tenant_id=$1 AND r.id=$2 AND r.graph_sealed AND r.status='queued' AND NOT r.cancel_requested
   AND i.principal_type='user' AND i.principal_id=$3 AND i.route='V2.Admit' AND i.request_hash=$4
   AND i.response_status=202 AND i.response_body=jsonb_build_object('run_id',r.id,'workflow_id',w.id,'state','queued'))
   AND (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued')=1
   AND (SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued')=1
   AND NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state<>'ready')`, original.request.TenantID, original.request.RunID, original.request.ActorID, original.request.RequestSHA).Scan(&valid)
	if err != nil || !valid {
		t.Fatal("real committed seal not visible", valid, err)
	}
	var snapshot string
	err = owner.QueryRow(ctx, `SELECT jsonb_build_object(
  'run',(SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=$1 AND id=$2),
  'workflow',(SELECT to_jsonb(w) FROM photo.v2_workflows w WHERE tenant_id=$1 AND run_id=$2),
  'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM photo.tasks t WHERE tenant_id=$1 AND run_id=$2),
  'inputs',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.task_id,i.ordinal) FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id WHERE t.tenant_id=$1 AND t.run_id=$2),
  'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM photo.backend_documents d WHERE tenant_id=$1 AND run_id=$2),
  'receipt',(SELECT jsonb_agg(to_jsonb(i) ORDER BY key) FROM photo.idempotency_records i WHERE tenant_id=$1 AND resource_id=$2),
  'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY cursor) FROM photo.run_events e WHERE tenant_id=$1 AND run_id=$2),
  'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY event_id) FROM photo.outbox o WHERE tenant_id=$1 AND payload->>'run_id'=$2::text),
  'quota',(SELECT to_jsonb(q) FROM photo.quota_reservations q WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2),
  'usage',(SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE tenant_id=$1)
 )::text`, original.request.TenantID, original.request.RunID).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
