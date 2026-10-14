package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func probeNativeAllTargetHandoffLifecycle(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) {
	t.Helper()
	var ready bool
	if err := owner.QueryRow(ctx, "SELECT photo.v27_native_automatic_handoff_ready($1,$2)", original.request.TenantID, original.shell.Run).Scan(&ready); err != nil || !ready {
		t.Fatal("handoff positive baseline", ready, err)
	}

	for _, test := range nativeHandoffMutationCases {
		t.Run(test.name, func(t *testing.T) {
			tx, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			changed, err := tx.Exec(ctx, test.sql, original.request.TenantID, original.request.RunID)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatal("lifecycle mutation", changed, err)
			}
			if err := tx.QueryRow(ctx, "SELECT photo.v27_native_automatic_handoff_ready($1,$2)", original.request.TenantID, original.shell.Run).Scan(&ready); err != nil || ready {
				t.Fatal("stale target handoff accepted", ready, err)
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
				t.Fatal(err)
			}
			token := uuid.New()
			if _, err := tx.Exec(ctx, "UPDATE photo.runs SET v27_initial_recovery_done=false,v27_initial_recovery_token=$3,v27_initial_recovery_until=clock_timestamp()+interval '1 minute' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run, token); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed',$4)", original.request.TenantID, original.shell.Run, token, original.policy.body).Scan(&ready); err != nil || ready {
				t.Fatal("recorder retired stale target", ready, err)
			}
		})
	}
	if err := owner.QueryRow(ctx, "SELECT photo.v27_native_automatic_handoff_ready($1,$2)", original.request.TenantID, original.shell.Run).Scan(&ready); err != nil || !ready {
		t.Fatal("rollback changed authority", ready, err)
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_TARGET_LIFECYCLE_REFUSAL_OK")
}

var nativeHandoffMutationCases = []struct{ name, sql string }{
	{"target_root_expired", `UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=(SELECT (e->'protection'->'ref'->>'artifact_id')::uuid FROM photo.runs r CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v27_native_targets_transfer'->'targets') e WHERE r.tenant_id=$1 AND r.id=$2 AND e->'reuse_reference'='false'::jsonb LIMIT 1)`},
	{"target_asset_deleted", `UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=$1 AND id=(SELECT v.asset_id FROM photo.runs r CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v27_native_targets_transfer'->'targets') e JOIN photo.asset_versions v ON v.tenant_id=r.tenant_id AND v.id=(e->'source'->>'asset_version_id')::uuid WHERE r.tenant_id=$1 AND r.id=$2 AND e->'reuse_reference'='false'::jsonb LIMIT 1)`},
	{"target_producer_cancelled", `UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=(SELECT (e->'producer'->>'run_id')::uuid FROM photo.runs r CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v27_native_targets_transfer'->'targets') e WHERE r.tenant_id=$1 AND r.id=$2 AND e->'reuse_reference'='false'::jsonb LIMIT 1)`},
}
