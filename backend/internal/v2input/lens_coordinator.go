package v2input

import (
	"context"
	"github.com/google/uuid"
)

type LensTaskStager interface {
	Stage(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, FrozenLensTask) error
}
type LensInputRegistrar interface {
	RegisterLensInputs(context.Context, uuid.UUID, uuid.UUID, PreparedLensTask) error
}
type LensPreparationCoordinator struct {
	Documents DocumentPublisher
	Stage     LensTaskStager
	Inputs    LensInputRegistrar
}

// Resume finishes one frozen task in an existing authorized draft run. All IO
// phases are replayable and stop at the first error; SQL rechecks live authority.
// Successful preparation leaves dispatch gated and does not seal the graph.
func (c LensPreparationCoordinator) Resume(ctx context.Context, tenant, run uuid.UUID, request LensTaskRequest) (PreparedLensTask, error) {
	var empty PreparedLensTask
	if c.Documents == nil || c.Stage == nil || c.Inputs == nil || tenant == uuid.Nil || run == uuid.Nil {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	workflow, err := WorkflowID(run)
	if err != nil || request.Workflow != workflow {
		return empty, ErrInvalid
	}
	frozen, err := CompileLensTask(request)
	if err != nil {
		return empty, err
	}
	plan, err := frozen.DocumentPlan()
	if err != nil {
		return empty, err
	}
	published, err := PublishLensDocuments(ctx, c.Documents, tenant, run, plan)
	if err != nil {
		return empty, err
	}
	request.ParamsRef = published.Params
	request.OriginalRef = published.Original
	frozen, err = CompileLensTask(request)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = c.Stage.Stage(ctx, tenant, run, workflow, request.Ordinal, frozen); err != nil {
		return empty, err
	}
	prepared, err := PublishLensTask(ctx, frozen, ScopedPublisher{Documents: c.Documents, Tenant: tenant, Run: run})
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = c.Inputs.RegisterLensInputs(ctx, tenant, run, prepared); err != nil {
		return empty, err
	}
	return prepared, nil
}
