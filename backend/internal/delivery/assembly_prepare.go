package delivery

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// PrepareObservedAssembly reads the complete admitted matrix and publishes its
// shards under the current expansion lease. It returns the root bytes for the
// dedicated root publication/admission step; it never dispatches assembly.
// Publication callbacks must repeat SQL authority, including terminal exports.
func (p PreparePlan) PrepareObservedAssembly(ctx context.Context, store MatrixObservationStore, lease ExpansionLease, publish PublishDocument) (AssemblyInput, []byte, string, string, error) {
	if publish == nil {
		return AssemblyInput{}, nil, "", "", v2input.ErrInvalid
	}
	observations, state, err := p.ReadObservedMatrix(ctx, store, lease)
	if err != nil {
		return AssemblyInput{}, nil, "", "", err
	}
	if state != "assemble" {
		return AssemblyInput{}, nil, "", state, nil
	}
	fencedPublish := func(ctx context.Context, purpose string, body []byte) (v2input.Ref, error) {
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		if err := store.Renew(ctx, lease); err != nil {
			return v2input.Ref{}, err
		}
		return publish(ctx, purpose, body)
	}
	input, body, hash, err := p.BuildObservedAssemblyInput(ctx, lease.Run, observations, fencedPublish)
	if err != nil {
		return AssemblyInput{}, nil, "", "", err
	}
	if err := ctx.Err(); err != nil {
		return AssemblyInput{}, nil, "", "", err
	}
	if err := store.Renew(ctx, lease); err != nil {
		return AssemblyInput{}, nil, "", "", err
	}
	return input, body, hash, state, nil
}
