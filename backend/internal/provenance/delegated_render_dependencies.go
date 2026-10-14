package provenance

import (
	"context"

	"photoeditor/backend/internal/canonical"
)

// Dependency claims are derived from a fresh delegated causal read, never
// from a caller-supplied inventory alone. The causal reader repeats adopted
// render, closure and producer authority around external IO.
type BoundDelegatedRenderDependencyResolver struct {
	Causal BoundDelegatedRenderCausalSource
}

func (s BoundDelegatedRenderDependencyResolver) ResolveDelegatedRenderDependencies(
	ctx context.Context, ready ReadySigningExport, task CanonicalSigningTask,
	inventory SigningRenderCausalInventory) ([]SigningRenderDependencyClaim, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	current, err := s.Causal.ReadDelegatedRenderCausal(ctx, ready, task)
	if err != nil { return nil, err }
	expected, err := canonical.Hash(inventory)
	if err != nil { return nil, err }
	actual, err := canonical.Hash(current.inventory)
	if err != nil || actual != expected ||
		inventory.taskDigest != current.inventory.taskDigest { return nil, ErrInvalid }
	return compileSigningRenderDependencyClaims(current.inventory)
}

var _ DelegatedRenderDependencyReader = BoundDelegatedRenderDependencyResolver{}
