package provenance

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// This joins accepted review/action/output claims to the same physical causal
// state. It is not a complete non-pixel ancestry resolver or signer installation.
type signingRenderReviewedCausalReceipt struct {
	causal  signingRenderCausalReceipt
	outputs ResolvedSigningRenderOutputs
}
type BoundSigningRenderReviewedCausalSource struct {
	Causal  BoundSigningRenderCausalSource
	Reviews SigningRenderReviewReader
}
type signingRenderDigestReader struct {
	Source SigningRenderEvidenceReader
	Digest string
}

func (s signingRenderDigestReader) ReadSigningRenderEvidence(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (SigningRenderEvidence, error) {
	var zero SigningRenderEvidence
	if s.Source == nil || !digest(s.Digest) {
		return zero, ErrInvalid
	}
	evidence, err := s.Source.ReadSigningRenderEvidence(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	actual, err := signingRenderEvidenceDigest(evidence)
	if err != nil || actual != s.Digest {
		return zero, ErrInvalid
	}
	return evidence, nil
}
func (s BoundSigningRenderReviewedCausalSource) read(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (signingRenderReviewedCausalReceipt, error) {
	var zero signingRenderReviewedCausalReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Reviews == nil {
		return zero, ErrInvalid
	}
	owned, err := ownSigningProducerTask(task)
	if err != nil {
		return zero, err
	}
	causal, err := s.Causal.read(ctx, who, ready, owned)
	if err != nil {
		return zero, err
	}
	outputs, err := ResolveSigningRenderOutputs(ctx, signingRenderDigestReader{Source: s.Causal.Evidence, Digest: causal.renderDigest}, s.Reviews, who, ready, owned)
	if err != nil {
		return zero, err
	}
	current, err := s.Causal.read(ctx, who, ready, owned)
	if err != nil {
		return zero, err
	}
	if causal.stamp != current.stamp {
		return zero, ErrInvalid
	}
	before, err := canonical.Hash([]any{causal.pixels.inputs, causal.pixels.producers, outputs})
	if err != nil {
		return zero, err
	}
	finalOutputs, err := ResolveSigningRenderOutputs(ctx, signingRenderDigestReader{Source: s.Causal.Evidence, Digest: current.renderDigest}, s.Reviews, who, ready, owned)
	if err != nil {
		return zero, err
	}
	after, err := canonical.Hash([]any{current.pixels.inputs, current.pixels.producers, finalOutputs})
	if err != nil || before != after {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return signingRenderReviewedCausalReceipt{causal: current, outputs: finalOutputs}, nil
}
