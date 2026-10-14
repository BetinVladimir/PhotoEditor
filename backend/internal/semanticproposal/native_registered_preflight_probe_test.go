package semanticproposal

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func probeNativeRegisteredPreflight(t *testing.T, ctx context.Context, owner *pgxpool.Pool, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation, built v2input.Built) {
	t.Helper()
	var artifact uuid.UUID
	var expires *time.Time
	err := owner.QueryRow(ctx, `SELECT a.id,a.expires_at FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=$1 AND d.run_id=$2 AND d.purpose='v2_input_manifest' AND d.owner_id=(SELECT min(id::text)::uuid FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2)`, original.request.TenantID, original.request.RunID).Scan(&artifact, &expires)
	if err != nil {
		t.Fatal("registered manifest fixture", err)
	}
	probeNativeRegisteredPreflightLocks(t, ctx, owner, p, who, original, plan, c, built, artifact)
	if _, err = owner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, artifact); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := owner.Exec(cleanup, "UPDATE photo.artifacts SET expires_at=$3 WHERE tenant_id=$1 AND id=$2", original.request.TenantID, artifact, expires); err != nil {
			t.Error("restore fixture expiry", err)
		}
	}()
	before := observeNativeMixedParent(t, ctx, owner, original, true)
	spy := &nativeRefusedRepairPublisher{}
	p.TransportDocuments = spy
	if err := p.repairNativeAllTargetTransfer(ctx, who, original.request, original.published, plan, c, built); err == nil || spy.calls != 0 {
		t.Fatal("expired registered transport reached publisher", err, spy.calls)
	}
	if observeNativeMixedParent(t, ctx, owner, original, true) != before {
		t.Fatal("registered preflight refusal changed parent")
	}
	t.Log("V27_NATIVE_REGISTERED_PREFLIGHT_NO_PUBLICATION_OK")
}
