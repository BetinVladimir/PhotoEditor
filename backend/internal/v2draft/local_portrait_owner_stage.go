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

// Stage persists the exact frozen plan and one blocked task after Params ready.
// It never publishes closure documents, registers inputs or seals/queues a graph.
func (s SQLLocalPortraitOwnerScope) Stage(ctx context.Context, tenant, run uuid.UUID, ordinal int, frozen FrozenLocalPortraitTask) error {
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
	plan, err := frozen.DocumentPlan()
	if err != nil {
		return err
	}
	task, err := v2input.TaskID(workflow, plan.Task.Operation, ordinal)
	if err != nil || task != plan.Task.ID {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(plan)
	if err != nil || len(body) > 1048576 {
		return v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return database.InTransaction(bounded, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(bounded, "SELECT photo.stage_v28_local_portrait_task($1,$2,$3)", tenant, run, string(body)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return bounded.Err()
	})
}
