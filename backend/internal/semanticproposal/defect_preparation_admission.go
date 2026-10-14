package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Admits an original or still-pending native producer from private compiler
// evidence. Completed admission recovery uses sealRegisteredDefectPreparation
// with the retained immutable closure instead of reserving transport again.
// No ticket is returned until the final activation transaction commits.
func (s SQLTransferSourceReader) admitPendingDefectPreparation(ctx context.Context, scheduler *pgxpool.Pool, publisher v2input.Publisher, who auth.Identity, plan defectPreparationDraftPlan, person personProtectionProof) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scheduler == nil || publisher == nil {
		return zero, v2input.ErrInvalid
	}
	// Independent person authority must be live before creating the draft or
	// publishing transport; final activation repeats the locked SQL checks.
	prepared, err := s.prepareDefectPersonAdmission(ctx, who, plan, person)
	if err != nil {
		return zero, err
	}
	transport, err := s.persistDefectPreparationInputs(ctx, scheduler, publisher, who, plan)
	if err != nil {
		return zero, err
	}
	return s.sealRegisteredDefectPreparation(ctx, scheduler, who, prepared, transport.Graph.Built)
}
