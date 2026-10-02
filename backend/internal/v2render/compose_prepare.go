package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type CompositionInputResolver interface {
	ResolveTaskInputs(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) ([]v2input.Entry, error)
}

type CompositionPreparation struct {
	Tenant, Task   uuid.UUID
	Actor          auth.Identity
	Layer          Layer
	Base           v2input.SessionVersionBinding
	SourceResult   *v2input.Ref
	ProposalResult v2input.Ref
	ProtectedMask  *v2input.Ref
	Width, Height  int64
	ParamsRef      v2input.Ref
}

type PreparedComposition struct {
	Compiled    CompiledComposition
	InputDigest string
	GraphTask   v2input.GraphTask
}

// PrepareComposition expands already-published frozen params and parent inputs,
// then publishes transport documents outside any DB admission transaction. It
// returns a GraphTask ready for relational validation/atomic graph registration;
// it does not create shells, authorize null lineage, seal graphs or enqueue.
func PrepareComposition(ctx context.Context, request CompositionPreparation, resolver CompositionInputResolver, publisher v2input.Publisher) (PreparedComposition, error) {
	if err := ctx.Err(); err != nil {
		return PreparedComposition{}, err
	}
	if request.Tenant == uuid.Nil || request.Task == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil || resolver == nil || publisher == nil {
		return PreparedComposition{}, v2input.ErrInvalid
	}
	compiled, err := CompileComposition(request.Layer, request.Base, request.SourceResult, request.ProposalResult, request.ProtectedMask, request.Width, request.Height)
	if err != nil {
		return PreparedComposition{}, err
	}
	roots, err := CompositionInputRoots(compiled, request.ParamsRef)
	if err != nil {
		return PreparedComposition{}, err
	}
	resolved, err := resolver.ResolveTaskInputs(ctx, request.Actor, request.Tenant, roots, request.Base)
	if err != nil {
		return PreparedComposition{}, err
	}
	frozen, err := FreezeCompositionInputs(request.Task, compiled, request.ParamsRef, resolved)
	if err != nil {
		return PreparedComposition{}, err
	}
	graph, err := PublishCompositionInputs(ctx, request.Task, frozen, publisher)
	if err != nil {
		return PreparedComposition{}, err
	}
	return PreparedComposition{Compiled: compiled, InputDigest: frozen.InputDigest, GraphTask: graph}, nil
}
