package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type renderCoordinatorProbe struct {
	events      []string
	fail        string
	cancelPhase string
	cancel      context.CancelFunc
}

func (p *renderCoordinatorProbe) Begin(context.Context, uuid.UUID, uuid.UUID, auth.Identity, string, CompiledRenderRun) error {
	p.events = append(p.events, "begin")
	if p.fail == "begin" {
		return v2input.ErrExpired
	}
	if p.cancelPhase == "begin" {
		p.cancel()
	}
	return nil
}
func (p *renderCoordinatorProbe) Prepare(ctx context.Context, actor auth.Identity, tenant, run, task uuid.UUID, binding v2input.SessionVersionBinding, ref v2input.Ref, recipe, source []byte, resolver CompositionInputResolver) (v2input.GraphTask, error) {
	p.events = append(p.events, "publish")
	if p.fail == "publish" {
		return v2input.GraphTask{}, v2input.ErrExpired
	}
	graph, err := PrepareRenderInputClosure(ctx, actor, tenant, task, binding, ref, recipe, source, resolver, &compositionTransportPublisher{})
	if p.cancelPhase == "publish" {
		p.cancel()
	}
	return graph, err
}
func (p *renderCoordinatorProbe) RegisterRenderInputs(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, v2input.DraftTask, v2input.GraphTask) error {
	p.events = append(p.events, "register")
	if p.fail == "register" {
		return v2input.ErrExpired
	}
	if p.cancelPhase == "register" {
		p.cancel()
	}
	return nil
}
