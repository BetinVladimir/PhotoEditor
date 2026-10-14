package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PublishAndRegisterTransport commits the blocked shell, publishes its bounded
// chunk/manifest outside SQL, then registers exact compiler inputs in a new
// transaction. Publication failures retain replayable scoped documents. Recovery
// after committed registration remains a separate DB-only operation.
func (s SQLDonorDescriptorStore) PublishAndRegisterTransport(ctx context.Context, who auth.Identity, tenant, run uuid.UUID,
	frozen FrozenDonorTask, documents v2input.DocumentPublisher) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if documents == nil {
		return zero, v2input.ErrInvalid
	}
	if err := s.Stage(ctx, who, tenant, run, frozen); err != nil {
		return zero, err
	}
	graph, err := frozen.PublishInputClosure(ctx, v2input.ScopedPublisher{Documents: documents, Tenant: tenant, Run: run})
	if err != nil {
		return zero, err
	}
	if err = s.RegisterTransport(ctx, who, tenant, run, frozen, graph); err != nil {
		return zero, err
	}
	return graph, nil
}
