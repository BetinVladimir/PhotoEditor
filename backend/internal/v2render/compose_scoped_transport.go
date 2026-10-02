package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type CompositionTransportPublisher struct{ Documents v2input.DocumentPublisher }

// Publish uses the existing durable backend-document reservation lifecycle for
// an already staged task. It derives the task from run/ordinal, publishes outside
// admission transactions and leaves SQL input registration as a separate phase.
func (p CompositionTransportPublisher) Publish(ctx context.Context, tenant, run uuid.UUID, ordinal int, frozen FrozenCompositionInputs) (v2input.GraphTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	if p.Documents == nil || tenant == uuid.Nil || run == uuid.Nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	task, err := v2input.TaskID(workflow, "layer.compose", ordinal)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	return PublishCompositionInputs(ctx, task, frozen, v2input.ScopedPublisher{Documents: p.Documents, Tenant: tenant, Run: run})
}
