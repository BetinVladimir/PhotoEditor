package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeRefusedRepairPublisher struct{ calls int }

func (p *nativeRefusedRepairPublisher) Publish(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
	p.calls++
	return artifactjson.Ref{}, errors.New("expired reservation reached publisher")
}

// Isolated fixture owner commits only the expiration so the real scheduler
// connection sees it. Restore the exact expiry before the successful job retry.
// Caller retains the original parent advisory lock throughout.
func probeNativeExpiredTransferReservation(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, capacity TransferPreparation, built v2input.Built) {
	t.Helper()
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var expires time.Time
	if err := owner.QueryRow(ctx, "SELECT expires_at FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", original.request.TenantID, original.request.RunID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var body string
		err := owner.QueryRow(ctx, `SELECT jsonb_build_object(
   'run',(SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=$1 AND id=$2),
   'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM photo.tasks t WHERE tenant_id=$1 AND run_id=$2),
   'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM photo.backend_documents d WHERE tenant_id=$1 AND run_id=$2),
   'usage',(SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE tenant_id=$1),
   'receipt',(SELECT jsonb_agg(to_jsonb(i) ORDER BY key) FROM photo.idempotency_records i WHERE tenant_id=$1 AND resource_id=$2)
  )::text`, original.request.TenantID, original.request.RunID).Scan(&body)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	before := snapshot()
	changed, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", original.request.TenantID, original.request.RunID)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("expire saved reservation", changed, err)
	}
	restore := func() {
		repairCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		changed, err := owner.Exec(repairCtx, "UPDATE photo.quota_reservations SET expires_at=$3 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", original.request.TenantID, original.request.RunID, expires)
		if err != nil || changed.RowsAffected() != 1 {
			t.Error("restore fixture reservation", changed, err)
		}
	}
	defer restore()
	spy := &nativeRefusedRepairPublisher{}
	p.TransportDocuments = spy
	if err := p.repairNativeAllTargetTransfer(ctx, who, original.request, original.published, plan, capacity, built); err == nil {
		t.Fatal("expired reservation repaired")
	}
	if spy.calls != 0 {
		t.Fatal("expired reservation invoked publisher", spy.calls)
	}
	if after := snapshot(); after != before {
		t.Fatal("refused repair changed run/task/document/usage/receipt state")
	}
	t.Log("V27_NATIVE_ALL_TARGET_EXPIRED_RESERVATION_NO_PUBLICATION_OK")
}
