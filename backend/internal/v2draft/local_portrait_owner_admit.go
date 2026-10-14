package v2draft

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Admit registers only compiler-owned inputs and seals in one DB transaction.
// SQL repeats current owner/qualification/registry gates while holding locks.
// No object reads/writes or qualification generation occur in this transaction.
func (s SQLLocalPortraitOwnerScope) Admit(ctx context.Context, tenant, run uuid.UUID, ordinal int,
	frozen FrozenLocalPortraitTask, transport v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return err
	}
	task, err := v2input.TaskID(workflow, draft.Operation, ordinal)
	if err != nil || task != draft.ID || transport.ID != task || transport.Built.Count != len(frozen.entries) {
		return v2input.ErrInvalid
	}
	entriesSHA, err := canonical.Hash(frozen.entries)
	if err != nil || entriesSHA != transport.Built.SHA256 || len(transport.Direct) != len(frozen.entries) {
		return v2input.ErrInvalid
	}
	direct := make([]v2input.Ref, len(frozen.entries))
	for i, entry := range frozen.entries {
		if transport.Direct[i] != entry.Ref {
			return v2input.ErrInvalid
		}
		direct[i] = entry.Ref
	}
	built := transport.Built
	built.Chunks = append([]v2input.Ref(nil), built.Chunks...)
	graph := v2input.GraphTask{ID: task, Built: built, Direct: direct, Open: func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &localPortraitCursor{entries: append([]v2input.Entry(nil), frozen.entries...)}, nil
	}}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return database.InTransaction(bounded, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return v2input.SealGraphInTransaction(bounded, tx, tenant, run, []v2input.GraphTask{graph})
	})
}
