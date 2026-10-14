package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type LocalPortraitTaskStager interface {
	Stage(context.Context, uuid.UUID, uuid.UUID, int, FrozenLocalPortraitTask) error
}

// PublishStagedLocalPortraitInputs publishes outside transactions, only after
// Stage commits. A failed upload leaves the blocked shell for retry/cleanup.
// Graph remains unregistered and unsealed until independent final admission.
func PublishStagedLocalPortraitInputs(ctx context.Context, stager LocalPortraitTaskStager,
	documents v2input.DocumentPublisher, tenant, run uuid.UUID, ordinal int, frozen FrozenLocalPortraitTask) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if stager == nil || documents == nil || tenant == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, draft.Operation, ordinal)
	if err != nil || task != draft.ID {
		return zero, v2input.ErrInvalid
	}
	if err = stager.Stage(ctx, tenant, run, ordinal, frozen); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return frozen.PublishInputClosure(ctx, v2input.ScopedPublisher{Documents: documents, Tenant: tenant, Run: run})
}
