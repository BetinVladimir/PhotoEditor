package semanticproposal

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

func nativeMixedRegistrationEnabled() bool {
	return os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION") == "1"
}

// Construct valid partial registration through normal draft/publication and
// scheduler registration APIs; no immutable registered row is changed.
func prepareNativeMixedRegistration(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation, built v2input.Built) {
	t.Helper()
	preparation := plan.reference.person.source.protection.published.preparation
	completed, err := p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, preparation.plan, c.Color, preparation.result, built)
	if err != nil {
		t.Fatal(err)
	}
	err = database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		_, err := p.Reader.beginNativeAllTargetTransferInTransaction(ctx, tx, who, original.request, original.requestKey, original.published, plan, c, completed, p.ReservedBytes)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: original.request.TenantID, Run: original.request.RunID}
	prepared, err := PublishWorkflowTasks(ctx, plan.core, publisher)
	if err != nil {
		t.Fatal(err)
	}
	transports := make([]v2input.Built, len(prepared))
	for i, task := range prepared {
		transports[i] = task.Graph.Built
	}
	graph, err := BindWorkflowTransport(ctx, plan.core, transports)
	if err != nil || len(graph) < 2 {
		t.Fatal("mixed graph needs two tasks", err)
	}
	if err = p.Reader.recheckNativeAllTargetTransferPhysical(ctx, who, original.request, plan, c); err != nil {
		t.Fatal(err)
	}
	completed, err = p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, preparation.plan, c.Color, preparation.result, built)
	if err != nil {
		t.Fatal(err)
	}
	first := 0
	for i := range graph {
		if graph[i].ID.String() < graph[first].ID.String() {
			first = i
		}
	}
	err = database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		if err := p.Reader.lockNativeAllTargetTransferAdmissionEvidence(ctx, tx, who, original.request, original.published, plan, c, completed); err != nil {
			return err
		}
		task := graph[first]
		return v2input.RegisterInTransaction(ctx, tx, original.request.TenantID, original.request.RunID, task.ID, task.Built, task.Open, task.Direct)
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var task uuid.UUID
	if err := owner.QueryRow(ctx, "SELECT id FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 ORDER BY id OFFSET 1 LIMIT 1", original.request.TenantID, original.request.RunID).Scan(&task); err != nil {
		t.Fatal("mixed fixture needs two tasks", err)
	}
	// A stray metadata field must be rejected before repair publishes anything.
	if _, err = owner.Exec(ctx, "UPDATE photo.tasks SET execution_profile=execution_profile||jsonb_build_object('contracts_sha',repeat('a',64)) WHERE tenant_id=$1 AND id=$2", original.request.TenantID, task); err != nil {
		t.Fatal(err)
	}
	before := observeNativeMixedParent(t, ctx, owner, original, true)
	spy := &nativeRefusedRepairPublisher{}
	refused := p
	refused.TransportDocuments = spy
	if err = refused.repairNativeAllTargetTransfer(ctx, who, original.request, original.published, plan, c, built); err == nil || spy.calls != 0 {
		t.Fatal("corrupt mixed registration published", err, spy.calls)
	}
	if observeNativeMixedParent(t, ctx, owner, original, true) != before {
		t.Fatal("refused mixed repair changed state")
	}
	if _, err = owner.Exec(ctx, "UPDATE photo.tasks SET execution_profile=execution_profile-'contracts_sha' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, task); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, "UPDATE photo.tasks SET inputs=jsonb_build_array(jsonb_build_object('artifact_id',$3::uuid,'sha256',$4::text)) WHERE tenant_id=$1 AND id=$2", original.request.TenantID, task, original.request.Intent.Reference.Source.ArtifactID, original.request.Intent.Reference.Source.SHA256); err != nil {
		t.Fatal(err)
	}
	before = observeNativeMixedParent(t, ctx, owner, original, true)
	if err = refused.repairNativeAllTargetTransfer(ctx, who, original.request, original.published, plan, c, built); err == nil || spy.calls != 0 {
		t.Fatal("stale task inputs reached publisher", err, spy.calls)
	}
	if observeNativeMixedParent(t, ctx, owner, original, true) != before {
		t.Fatal("stale input refusal changed state")
	}
	if _, err = owner.Exec(ctx, "UPDATE photo.tasks SET inputs='[]'::jsonb WHERE tenant_id=$1 AND id=$2", original.request.TenantID, task); err != nil {
		t.Fatal(err)
	}
	probeNativeRegisteredPreflight(t, ctx, owner, p, who, original, plan, c, built)
	t.Log("V27_NATIVE_ALL_TARGET_MIXED_STALE_INPUTS_NO_PUBLICATION_OK")
	t.Log("V27_NATIVE_ALL_TARGET_MIXED_CORRUPT_NO_PUBLICATION_OK")
	t.Log("V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION_PREPARED_OK")
}

func observeNativeMixedParent(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan, full bool) string {
	t.Helper()
	var snapshot string
	err := owner.QueryRow(ctx, `SELECT jsonb_build_object(
 'profiles',(SELECT jsonb_agg(jsonb_build_object('id',id,'profile',execution_profile,'direct_inputs',inputs) ORDER BY id) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND ($3 OR id=(SELECT min(id::text)::uuid FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2))),
 'inputs',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.task_id,i.ordinal) FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id WHERE t.tenant_id=$1 AND t.run_id=$2 AND ($3 OR t.id=(SELECT min(id::text)::uuid FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2))),
 'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM photo.backend_documents d WHERE tenant_id=$1 AND run_id=$2),
 'reserved',(SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2))::text`, original.request.TenantID, original.request.RunID, full).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
