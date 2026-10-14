package semanticproposal

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/v2input"
)

// PublishWorkflowTasks persists each frozen transfer closure outside SQL.
// Publisher reservations must be scoped to this plan's tenant/run and replay-safe.
// Partial durable publications remain recoverable; no partial graph is returned.
// This method neither registers inputs nor seals or activates a workflow.
func PublishWorkflowTasks(ctx context.Context, plan WorkflowPlan, publisher v2input.Publisher) ([]v2input.PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if publisher == nil || len(plan.frozen) == 0 || len(plan.frozen) != len(plan.tasks) {
		return nil, v2input.ErrInvalid
	}
	// Validate the entire batch before the first publication.
	for i, frozen := range plan.frozen {
		task, err := frozen.DraftTask()
		if err != nil {
			return nil, err
		}
		expected := plan.tasks[i]
		if task.ID != expected.ID || task.NodeKey != expected.NodeKey || task.Operation != expected.Operation || task.OperationVersion != expected.OperationVersion || task.ResourceClass != expected.ResourceClass || task.ParamsArtifactID != expected.ParamsArtifactID || task.ParamsSHA != expected.ParamsSHA || task.InputDigest != expected.InputDigest || !bytes.Equal(task.ExecutionProfile, expected.ExecutionProfile) {
			return nil, v2input.ErrInvalid
		}
	}
	prepared := make([]v2input.PreparedSemanticTask, len(plan.frozen))
	for i, frozen := range plan.frozen {
		value, err := v2input.PublishSemanticTransferTask(ctx, frozen, publisher)
		if err != nil {
			return nil, err
		}
		if value.Graph.ID != plan.tasks[i].ID || value.InputDigest != plan.tasks[i].InputDigest {
			return nil, v2input.ErrInvalid
		}
		prepared[i] = value
	}
	return prepared, nil
}
