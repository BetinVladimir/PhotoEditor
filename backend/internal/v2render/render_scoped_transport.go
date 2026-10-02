package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type RenderTransportPublisher struct{ Documents v2input.DocumentPublisher }

// Prepare publishes through the durable backend-document lifecycle for the
// supplied staged task. SQL reservation validates its actual run/tenant owner.
// The caller must commit the task shell before this boundary; graph registration
// and full-render operation/provider admission remain separate phases.
func (p RenderTransportPublisher) Prepare(ctx context.Context, actor auth.Identity, tenant, run, task uuid.UUID, binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw []byte, resolver CompositionInputResolver) (v2input.GraphTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	if p.Documents == nil || run == uuid.Nil || tenant == uuid.Nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	return PrepareRenderInputClosure(ctx, actor, tenant, task, binding, recipeRef, recipeRaw, sourceRaw, resolver, v2input.ScopedPublisher{Documents: p.Documents, Tenant: tenant, Run: run})
}
