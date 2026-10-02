package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type SQLOriginalCompositionDraftStore struct {
	DB *SQLOriginalCompositionPreflight
}

// Begin reserves output bytes exactly once and persists the immutable original-
// source run/workflow plan. SQL repeats live scope under quota/snapshot locks.
// No task, queue message or graph seal is created at this stage.
func (s SQLOriginalCompositionDraftStore) Begin(ctx context.Context, tenant, run uuid.UUID, who auth.Identity, policySHA string, draft CompiledCompositionDraft) error {
	if s.DB == nil || len(policySHA) != 64 {
		return v2input.ErrInvalid
	}
	for _, c := range policySHA {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return v2input.ErrInvalid
		}
	}
	if err := s.DB.CheckDraft(ctx, tenant, run, who, draft); err != nil {
		return err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return err
	}
	return database.InTransaction(ctx, s.DB.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.begin_v25_original_composition_draft($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenant, draft.Config.ProjectID, draft.Config.SnapshotID, who.UserID, who.SessionID, run, workflow, string(draft.Body), policySHA, draft.OutputBytes).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}
