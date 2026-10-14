package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// BindWorkflowTransport reconstructs DB-only graph cursors from the private
// frozen plan, rather than trusting mutable PreparedSemanticTask fields.
// It does not authorize sealing; final SQL scope checks remain mandatory.
func BindWorkflowTransport(ctx context.Context, plan WorkflowPlan, transports []v2input.Built) ([]v2input.GraphTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(transports) == 0 || len(transports) != len(plan.frozen) || len(plan.tasks) != len(plan.frozen) {
		return nil, v2input.ErrInvalid
	}
	graph := make([]v2input.GraphTask, len(transports))
	for i, built := range transports {
		prepared, err := plan.frozen[i].BindPublishedTransport(ctx, built)
		if err != nil {
			return nil, err
		}
		if prepared.Graph.ID != plan.tasks[i].ID || prepared.InputDigest != plan.tasks[i].InputDigest {
			return nil, v2input.ErrInvalid
		}
		graph[i] = prepared.Graph
	}
	return graph, nil
}
