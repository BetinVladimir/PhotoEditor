package semanticproposal

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Observation only. Succeeded still requires complete physical result readers
// and admission locks; this cannot produce a queued parent ticket.
type nativeInputSavedStageState struct {
	task                uuid.UUID
	taskState, runState string
	sealed              bool
}

func (s SQLTransferSourceReader) readNativeInputSavedStageState(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref, operation string) (nativeInputSavedStageState, error) {
	var zero, out nativeInputSavedStageState
	original, err := s.readNativeInputSavedStage(ctx, who, request, intent, snapshot, operation)
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT task_id,task_state,run_state,graph_sealed FROM photo.semantic_native_input_saved_stage_state($1,$2,$3,$4,$5,$6,$7,$8)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.RunID, original.run, operation, original.configSHA).Scan(&out.task, &out.taskState, &out.runState, &out.sealed)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrConflict
	}
	if err != nil {
		return zero, err
	}
	if out.task != original.task || len(out.taskState) < 1 || len(out.taskState) > 32 || len(out.runState) < 1 || len(out.runState) > 32 {
		return zero, v2input.ErrConflict
	}
	if _, err := nativeInputStagePreparationPhase(out); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return out, nil
}
