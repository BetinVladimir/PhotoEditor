package provenance

import (
	"context"
	"errors"
	"testing"
)

func TestDelegatedRenderDependenciesRequireCausalReader(t *testing.T) {
	reader := BoundDelegatedRenderDependencyResolver{}
	if _, err := reader.ResolveDelegatedRenderDependencies(context.Background(),
		ReadySigningExport{}, CanonicalSigningTask{}, SigningRenderCausalInventory{}); err == nil {
		t.Fatal("unconfigured causal source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ResolveDelegatedRenderDependencies(ctx,
		ReadySigningExport{}, CanonicalSigningTask{}, SigningRenderCausalInventory{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
