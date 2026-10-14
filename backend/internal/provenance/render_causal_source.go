package provenance

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

type SigningRenderClosureReader interface {
	ReadSigningRenderClosure(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) (SigningRenderClosureEvidence, error)
}

// This stage joins physical dependencies to stable pixel producers. It does not
// grant model/control authority or produce a complete signing graph node.
type BoundSigningRenderCausalSource struct {
	Evidence  SigningRenderEvidenceReader
	Closure   SigningRenderClosureReader
	Producers SigningImageProducer
}
type signingRenderCausalReceipt struct {
	inventory    SigningRenderCausalInventory
	pixels       signingRenderPixelAncestry
	stamp        string
	renderDigest string
}

func (s BoundSigningRenderCausalSource) read(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (signingRenderCausalReceipt, error) {
	var zero signingRenderCausalReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Evidence == nil || s.Closure == nil || s.Producers == nil {
		return zero, ErrInvalid
	}
	// Own RawMessage fields before handing the task to independent adapters.
	owned, err := ownSigningProducerTask(task)
	if err != nil {
		return zero, err
	}
	var renderDigest string
	read := func() (SigningRenderCausalInventory, string, error) {
		if err := ctx.Err(); err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		isolated, err := ownSigningProducerTask(owned)
		if err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		render, err := s.Evidence.ReadSigningRenderEvidence(ctx, who, ready, isolated)
		if err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		// Freeze the complete evidence fingerprint before another adapter can reuse
		// pointers. Private receipt fields must be explicitly included in this hash.
		before, err := signingRenderEvidenceDigest(render)
		if err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		isolated, err = ownSigningProducerTask(owned)
		if err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		closure, err := s.Closure.ReadSigningRenderClosure(ctx, who, ready, isolated)
		if err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		after, err := signingRenderEvidenceDigest(render)
		if err != nil || before != after {
			return SigningRenderCausalInventory{}, "", ErrInvalid
		}
		inventory, err := CompileSigningRenderCausalInventory(owned, render, closure)
		if err != nil {
			return SigningRenderCausalInventory{}, "", err
		}
		renderDigest = before
		stamp, err := canonical.Hash([]any{before, closure.taskDigest, closure.inventory})
		return inventory, stamp, err
	}
	inventory, stamp, err := read()
	if err != nil {
		return zero, err
	}
	pixels, err := ResolveSigningRenderPixelAncestry(ctx, s.Producers, who, ready, owned, inventory)
	if err != nil {
		return zero, err
	}
	currentInventory, current, err := read()
	if err != nil {
		return zero, err
	}
	if stamp != current {
		return zero, ErrInvalid
	}
	// The final physical read can perform external I/O. Revalidate every pixel
	// edge afterwards, preserving input-to-producer bindings, not just a set of
	// parent IDs (two producers swapping inputs would leave that set unchanged).
	finalPixels, err := ResolveSigningRenderPixelAncestry(ctx, s.Producers, who, ready, owned, currentInventory)
	if err != nil {
		return zero, err
	}
	if len(pixels.inputs) != len(finalPixels.inputs) || len(pixels.producers) != len(finalPixels.producers) {
		return zero, ErrInvalid
	}
	for i := range pixels.inputs {
		if pixels.inputs[i] != finalPixels.inputs[i] || pixels.producers[i] != finalPixels.producers[i] {
			return zero, ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return signingRenderCausalReceipt{inventory: inventory, pixels: pixels, stamp: stamp, renderDigest: renderDigest}, nil
}

func signingRenderEvidenceDigest(e SigningRenderEvidence) (string, error) {
	return canonical.Hash([]any{e.taskDigest, e.pixels.root, e.pixels.recipe,
		e.pixels.result, e.pixels.image, e.pixels.changedPixels, e.pixels.generatedPixels,
		e.report, e.parents, e.inheritedReview, e.qualification, e.dependencies})
}
