package provenance

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// Pixel ancestry is deliberately private and incomplete until model/background
// and authored-control ancestry are independently resolved by the full resolver.
type signingRenderPixelAncestry struct {
	inputs    []v2input.Ref
	parents   []uuid.UUID
	producers []uuid.UUID
}

func ResolveSigningRenderPixelAncestry(ctx context.Context, producers SigningImageProducer, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, inventory SigningRenderCausalInventory) (signingRenderPixelAncestry, error) {
	var zero signingRenderPixelAncestry
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(task)
	if err != nil || producers == nil || task.Task == uuid.Nil || task.Operation != "render.compose" || task.Version != "1" || digest != inventory.taskDigest || len(inventory.Images) < 1 {
		return zero, ErrInvalid
	}
	inputs := append([]v2input.Ref(nil), inventory.Images...)
	inputs = append(inputs, inventory.Results...)
	if len(inputs) > 4096 {
		return zero, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	parents := map[uuid.UUID]bool{}
	bound := make([]uuid.UUID, len(inputs))
	for _, input := range inputs {
		if !ref(input) || seen[input.ArtifactID] {
			return zero, ErrInvalid
		}
		seen[input.ArtifactID] = true
	}
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		producer, err := producers.ResolveSigningImageProducer(ctx, who, ready, input)
		if err != nil {
			return zero, err
		}
		if producer == uuid.Nil || producer == task.Task {
			return zero, ErrInvalid
		}
		bound[i] = producer
		parents[producer] = true
	}
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		producer, err := producers.ResolveSigningImageProducer(ctx, who, ready, input)
		if err != nil {
			return zero, err
		}
		if producer != bound[i] {
			return zero, ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	ordered := make([]uuid.UUID, 0, len(parents))
	for id := range parents {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	return signingRenderPixelAncestry{inputs: inputs, parents: ordered, producers: bound}, nil
}
