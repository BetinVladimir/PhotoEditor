package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type renderCoordinatorProbe struct {
	events      []string
	fail        string
	cancelPhase string
	cancel      context.CancelFunc
}

type renderMutatingCallerResolver struct {
	CompositionInputResolver
	mutate func()
}

func (r *renderMutatingCallerResolver) ResolveTaskInputs(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	if r.mutate != nil {
		mutate := r.mutate
		r.mutate = nil
		mutate()
	}
	return r.CompositionInputResolver.ResolveTaskInputs(ctx, who, tenant, roots, binding)
}

func (r *renderMutatingCallerResolver) ResolveTaskInputsWithWitnesses(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) ([]v2input.Entry, []v2draft.DocumentWitness, error) {
	if r.mutate != nil {
		mutate := r.mutate
		r.mutate = nil
		mutate()
	}
	provider, ok := r.CompositionInputResolver.(renderWitnessResolver)
	if !ok {
		return nil, nil, v2input.ErrInvalid
	}
	return provider.ResolveTaskInputsWithWitnesses(ctx, who, tenant, roots, binding)
}

func (r *renderMutatingCallerResolver) ResolveTaskInputEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error) {
	if r.mutate != nil {
		mutate := r.mutate
		r.mutate = nil
		mutate()
	}
	provider, ok := r.CompositionInputResolver.(renderEvidenceResolver)
	if !ok {
		return v2draft.TaskInputEvidence{}, v2input.ErrInvalid
	}
	return provider.ResolveTaskInputEvidence(ctx, who, tenant, roots, binding)
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

// Count real scoped evidence passes rather than replacing them with fixtures.
type renderCountingEvidenceResolver struct {
	CompositionInputResolver
	calls         int
	afterEvidence context.CancelFunc
}

func (r *renderCountingEvidenceResolver) ResolveTaskInputEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, roots []v2input.Entry, binding v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error) {
	r.calls++
	provider, ok := r.CompositionInputResolver.(renderEvidenceResolver)
	if !ok {
		return v2draft.TaskInputEvidence{}, v2input.ErrInvalid
	}
	evidence, err := provider.ResolveTaskInputEvidence(ctx, who, tenant, roots, binding)
	if r.afterEvidence != nil {
		r.afterEvidence()
	}
	return evidence, err
}
