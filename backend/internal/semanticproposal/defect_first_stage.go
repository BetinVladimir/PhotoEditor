package semanticproposal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Private server-selected inputs to the reference preparation stage. This is
// not a public request DTO; callers must reconstruct qualified execution and
// independent person coverage before choosing admission.
type firstDefectPreparation struct {
	execution                  defectPolicyExecutionPreparation
	person                     personProtectionProof
	costs                      v2input.SemanticQualifiedCosts
	ram, vram, scratch, output int64
	reserved                   int64
}

// Produces an internal child-stage acceptance, never the public parent ticket.
// Ready transport is reconstructed after durable admission; writable transport
// is used only for original/pending admission. A receipt selects the path, but
// full live evidence checks on that path remain the admission authority.
func (p TransferDraftPreparer) prepareAndAdmitFirstDefectStage(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, native firstDefectPreparation) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.TransportDocuments == nil {
		return zero, v2input.ErrInvalid
	}
	plan, err := p.Reader.prepareFirstDefectPlan(ctx, who, request, key, published, snapshot, snapshotBody, native.execution, p.Configuration.Color, p.Documents, native.costs, native.ram, native.vram, native.scratch, native.output, native.reserved)
	if err != nil {
		return zero, fmt.Errorf("rebuild original defect draft: %w", err)
	}
	// Validate independent person authority before observing the saved receipt.
	if _, err := p.Reader.prepareDefectPersonAdmission(ctx, who, plan, native.person); err != nil {
		return zero, fmt.Errorf("original defect person authority: %w", err)
	}
	var existing []byte
	if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", plan.request.TenantID.String()); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT photo.v2_admission_ticket($1,$2,$3,$4)", plan.request.TenantID, who.UserID, plan.key, plan.request.RequestSHA).Scan(&existing)
	}); err != nil {
		return zero, err
	}
	if len(existing) != 0 {
		return p.Reader.recoverCompletedDefectPreparation(ctx, p.Scheduler, who, plan, native.person)
	}
	publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: plan.request.TenantID, Run: plan.shell.Run}
	return p.Reader.admitPendingDefectPreparation(ctx, p.Scheduler, publisher, who, plan, native.person)
}
