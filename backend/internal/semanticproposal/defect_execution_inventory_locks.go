package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Composes declared-file stability with retained manual/parser/face/source
// evidence. Caller owns rollback, authorization/global policy/origin barriers
// and atomic admission. No object IO, commit or queued authority is introduced.
func (s SQLTransferSourceReader) lockDefectExecutionInventoryInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectMaskExecutionPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	// Reject incomplete retained manual evidence before acquiring any file locks.
	if _, err := s.decodeDefectExecutionManualProofs(ctx, who, tenant, binding, prepared); err != nil {
		return err
	}
	if prepared.face.originProof.pins != prepared.face.origin {
		return v2input.ErrInvalid
	}
	if err := s.lockDefectOriginPreparationProof(ctx, tx, who, tenant, binding, prepared.face.originProof); err != nil {
		return err
	}
	if err := s.lockDefectDeclaredFilesInTransaction(ctx, tx, who, tenant, binding, prepared.face); err != nil {
		return err
	}
	if err := s.lockDefectExecutionManualInTransaction(ctx, tx, who, tenant, binding, prepared); err != nil {
		return err
	}
	if err := s.lockDefectDeclaredFilesInTransaction(ctx, tx, who, tenant, binding, prepared.face); err != nil {
		return err
	}
	return s.lockDefectOriginPreparationProof(ctx, tx, who, tenant, binding, prepared.face.originProof)
}
