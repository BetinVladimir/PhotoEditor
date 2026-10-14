package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds the original parent lock and owns rollback. Publication precedes
// this transaction; the dedicated SQL barrier atomically seals and activates.
func (s SQLTransferSourceReader) sealNativeTargetFaceInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan, built, detectorBuilt v2input.Built) (v2input.JobTicket, error) {
	packet, err := s.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, plan, built, detectorBuilt)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return sealNativePreparationQueryInTransaction(ctx, tx, plan.shell.Tenant, plan.shell.Run, plan.shell.WorkflowID, packet, "SELECT photo.seal_semantic_target_face_preparation($1,$2,$3::jsonb)")
}
