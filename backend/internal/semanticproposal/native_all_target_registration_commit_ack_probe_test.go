package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

func nativeRegistrationCommitAckEnabled() bool {
	return os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_REGISTRATION_ACK") == "1"
}

func probeNativeRegistrationCommitAckLoss(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, capacity TransferPreparation, built v2input.Built, publisher v2input.Publisher) {
	t.Helper()
	phases, begins := 0, 0
	committed := false
	transaction := func(callback func(pgx.Tx) error) error {
		phases++
		if phases == 1 {
			return database.InTransaction(ctx, p.Scheduler, callback)
		}
		if phases != 2 {
			t.Fatal("registration unexpectedly retried")
		}
		return retryProposalTransaction(ctx, func(ctx context.Context) (pgx.Tx, error) {
			begins++
			tx, err := p.Scheduler.Begin(ctx)
			if err != nil {
				return nil, err
			}
			return nativeSealCommitAckLossTx{Tx: tx, committed: &committed}, nil
		}, callback)
	}
	prepared, err := p.Reader.persistNativeAllTargetTransferWithTransaction(ctx, transaction, publisher, who, original.request, original.requestKey, original.published, plan, capacity, built, p.ReservedBytes)
	if !errors.Is(err, errNativeSealCommitAckLost) || prepared != nil || !committed || phases != 2 || begins != 1 {
		t.Fatal("unknown registration commit exposed graph or retried", err, committed, phases, begins)
	}
	t.Log("V27_NATIVE_ALL_TARGET_REGISTRATION_COMMIT_ACK_LOST_OK")
}

// Observe independently committed blocked registration before recovery seals it.
func observeNativeCommittedRegistration(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) string {
	t.Helper()
	var valid bool
	err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=$1 AND id=$2 AND NOT graph_sealed AND status='draft' AND NOT cancel_requested)
 AND EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE tenant_id=$1 AND run_id=$2 AND (state<>'blocked' OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id)))
 AND NOT EXISTS(SELECT 1 FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued')
 AND NOT EXISTS(SELECT 1 FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued')`, original.request.TenantID, original.request.RunID).Scan(&valid)
	if err != nil || !valid {
		t.Fatal("committed registration not visible", valid, err)
	}
	return observeNativeRegistrationDocuments(t, ctx, owner, original)
}

func observeNativeRegistrationDocuments(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) string {
	t.Helper()
	var state string
	err := owner.QueryRow(ctx, `SELECT jsonb_build_object(
 'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM photo.backend_documents d WHERE tenant_id=$1 AND run_id=$2),
 'inputs',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.task_id,i.ordinal) FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id WHERE t.tenant_id=$1 AND t.run_id=$2),
 'reserved',(SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2))::text`, original.request.TenantID, original.request.RunID).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
