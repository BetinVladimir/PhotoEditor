package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Rebuilds a completed producer's input refs from the privately frozen compiler
// plan. Only ready immutable transport lookup is permitted; there is no upload,
// reservation, input registration or fallback to pending draft admission.
// The final completed SQL gate rechecks all original live authority and exact
// stored closure before returning the durable historical acceptance ticket.
func (s SQLTransferSourceReader) recoverCompletedDefectPreparation(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan defectPreparationDraftPlan, person personProtectionProof) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil || s.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	prepared, err := s.prepareDefectPersonAdmission(ctx, who, plan, person)
	if err != nil {
		return zero, err
	}
	ready := nativeReadyTransportPublisher{db: scheduler, who: who, tenant: plan.request.TenantID, run: plan.shell.Run, bucket: s.Bucket}
	transport, err := v2input.PublishSemanticDefectTask(ctx, plan.frozen, v2input.ScopedPublisher{Documents: ready, Tenant: plan.request.TenantID, Run: plan.shell.Run})
	if err != nil {
		return zero, err
	}
	return s.sealRegisteredDefectPreparation(ctx, scheduler, who, prepared, transport.Graph.Built)
}
