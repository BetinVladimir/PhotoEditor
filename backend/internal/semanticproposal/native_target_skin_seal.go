package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent lock and owns rollback. The dedicated SQL seal
// repeats complete admission before activation, or current evidence on replay.
func (s SQLTransferSourceReader) sealNativeTargetSkinInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetSkinDraftPlan, built, parserBuilt, detectorBuilt v2input.Built) (v2input.JobTicket, error) {
	packet, err := s.marshalNativeTargetSkinAdmissionEvidence(ctx, who, original, plan, built, parserBuilt, detectorBuilt)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativePreparationQueryInTransaction(ctx, tx, plan.shell.Tenant, plan.shell.Run, plan.shell.WorkflowID, packet, "SELECT photo.seal_semantic_target_skin_preparation($1,$2,$3::jsonb)")
}
