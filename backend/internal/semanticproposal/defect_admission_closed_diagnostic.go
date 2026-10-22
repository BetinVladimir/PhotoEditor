package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// Each projection has a separate savepoint: denied optional table reads cannot
// erase the three scheduler-callable pure-helper observations. All queries and
// rollback share ONE absolute four-second deadline; one second is reserved for
// restoration. No role changes, application writes, admission retries or IO.
func defectAdmissionClosedDiagnostic(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID, evidence string, original error) error {
	if original == nil {
		return nil
	}
	deadline := time.Now().Add(4 * time.Second)
	queryCtx, cancel := context.WithDeadline(ctx, deadline.Add(-time.Second))
	defer cancel()
	cleanupCtx, cleanupCancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cleanupCancel()
	observe := func(query string, args []any, values ...any) (string, bool) {
		nested, err := tx.Begin(queryCtx)
		if err != nil {
			return defectDiagnosticErrorStatus("begin", err), false
		}
		queryErr := nested.QueryRow(queryCtx, query, args...).Scan(values...)
		rollbackErr := nested.Rollback(cleanupCtx)
		if rollbackErr != nil {
			return defectDiagnosticErrorStatus("rollback", rollbackErr), false
		}
		if queryErr != nil {
			return defectDiagnosticErrorStatus("query", queryErr), true
		}
		return "observed", true
	}
	var closure, landmarks, roots bool
	pureStatus, usable := observe(`SELECT
 photo.semantic_defect_admission_closure_matches($1::jsonb->'files',$1::jsonb->'content',$1::jsonb->'registration') IS TRUE,
 photo.semantic_defect_landmark_inventory_matches($1::jsonb->'files',$1::jsonb->'content',$1::jsonb->'faces') IS TRUE,
 photo.semantic_defect_mask_root_inventory_matches($1::jsonb->'content',$1::jsonb->'parser',$1::jsonb->'manual') IS TRUE`, []any{evidence}, &closure, &landmarks, &roots)
	var tenantContext, scheduler, draft, oneTask, fivePolicies, liveReservation bool
	var policyExpectations, snapshotBinding, taskShape, graphEmpty, registryBackend, outputPolicyShape, reservationBudget bool
	catalogStatus := "not_attempted"
	if usable && queryCtx.Err() == nil {
		catalogStatus, _ = observe(`WITH observed AS MATERIALIZED (SELECT photo.semantic_defect_admission_catalog_diagnostic($1::uuid,$2::uuid,$3::jsonb) AS flags) SELECT (nullif(current_setting('app.tenant_id',true),'')=$1::uuid::text) IS TRUE,
 current_user='photo_scheduler',
 (flags->>'draft')::boolean,
 (flags->>'one_task')::boolean,
 (flags->>'five_output_policies')::boolean,
 (flags->>'live_reservation')::boolean,
 coalesce((flags->>'policy_expectations')::boolean,false),
 coalesce((flags->>'snapshot_binding')::boolean,false),
 coalesce((flags->>'task_shape')::boolean,false),
 coalesce((flags->>'graph_empty')::boolean,false),
 coalesce((flags->>'registry_backend')::boolean,false),
 coalesce((flags->>'output_policy_shape')::boolean,false),
 coalesce((flags->>'reservation_budget')::boolean,false)
 FROM observed`, []any{tenant, run, evidence}, &tenantContext, &scheduler, &draft, &oneTask, &fivePolicies, &liveReservation, &policyExpectations, &snapshotBinding, &taskShape, &graphEmpty, &registryBackend, &outputPolicyShape, &reservationBudget)
	}
	return fmt.Errorf("closed_diagnostic pure_status=%s closure=%s landmarks=%s roots=%s catalog_status=%s tenant_context=%s scheduler=%s draft=%s one_task=%s five_output_policies=%s live_reservation=%s policy_expectations=%s snapshot_binding=%s task_shape=%s graph_empty=%s registry_backend=%s output_policy_shape=%s reservation_budget=%s: %w", pureStatus, defectDiagnosticFact(pureStatus, closure), defectDiagnosticFact(pureStatus, landmarks), defectDiagnosticFact(pureStatus, roots), catalogStatus, defectDiagnosticFact(catalogStatus, tenantContext), defectDiagnosticFact(catalogStatus, scheduler), defectDiagnosticFact(catalogStatus, draft), defectDiagnosticFact(catalogStatus, oneTask), defectDiagnosticFact(catalogStatus, fivePolicies), defectDiagnosticFact(catalogStatus, liveReservation), defectDiagnosticFact(catalogStatus, policyExpectations), defectDiagnosticFact(catalogStatus, snapshotBinding), defectDiagnosticFact(catalogStatus, taskShape), defectDiagnosticFact(catalogStatus, graphEmpty), defectDiagnosticFact(catalogStatus, registryBackend), defectDiagnosticFact(catalogStatus, outputPolicyShape), defectDiagnosticFact(catalogStatus, reservationBudget), original)
}

// Unobserved booleans are unknown, never fabricated negative facts.
func defectDiagnosticFact(status string, value bool) string {
	if status != "observed" {
		return "unknown"
	}
	if value {
		return "true"
	}
	return "false"
}

// Only a fixed category is emitted, never the database error body or SQL.
func defectDiagnosticErrorStatus(stage string, err error) string {
	switch stage {
	case "begin", "query", "rollback":
	default:
		return "unavailable"
	}
	if errors.Is(err, context.Canceled) {
		return stage + "_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return stage + "_deadline"
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "42501":
			return stage + "_permission_denied"
		case "57014":
			return stage + "_cancelled"
		case "25P02":
			return stage + "_transaction_aborted"
		case "42702":
			return stage + "_ambiguous_reference"
		case "42703":
			return stage + "_undefined_column"
		case "42883":
			return stage + "_undefined_function"
		case "42601":
			return stage + "_invalid_query"
		}
	}
	return stage + "_unavailable"
}
