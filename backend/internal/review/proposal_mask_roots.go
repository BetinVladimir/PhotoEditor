package review

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

// ProposalFaceMaskRoot retains the exact verified producer document dependency.
// It is content evidence only; loader authorization and pixel checks are separate.
type ProposalFaceMaskRoot struct {
	Binding ProposalFaceMask
	Root    ArtifactRef
}

// ResolveProposalFaceMaskRoots verifies every declared skin/manual root in report
// order. The loader must discover and authorize the canonical producer root for
// the supplied mask. No caller-selected inventory or partial result is accepted.
func ResolveProposalFaceMaskRoots(ctx context.Context, raw []byte, proposal ArtifactRef, source DefectSource, load func(context.Context, ProposalFaceMask) (ArtifactRef, []byte, error)) ([]ProposalFaceMaskRoot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > 1048576 || len(source.TransformChain) > 1048576 {
		return nil, errors.New("face mask evidence exceeds byte limit")
	}
	raw = append([]byte(nil), raw...)
	source.TransformChain = append([]byte(nil), source.TransformChain...)
	bindings, err := ProposalFaceMasks(raw, proposal, source)
	if err != nil {
		return nil, err
	}
	if len(bindings) > 0 && load == nil {
		return nil, errors.New("missing face mask root loader")
	}
	result := make([]ProposalFaceMaskRoot, 0, len(bindings))
	seen := map[uuid.UUID]string{}
	for _, binding := range bindings {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Callback arguments must not alias the expected binding retained for the
		// comparison: an authorized storage adapter is still mutable application code.
		argument := binding
		argument.Mask.TransformChain = append([]byte(nil), binding.Mask.TransformChain...)
		if binding.ObjectID != nil {
			id := *binding.ObjectID
			argument.ObjectID = &id
		}
		if binding.Mask.ObjectID != nil {
			id := *binding.Mask.ObjectID
			argument.Mask.ObjectID = &id
		}
		if binding.Mask.ModelLockID != nil {
			lock := *binding.Mask.ModelLockID
			argument.Mask.ModelLockID = &lock
		}
		root, body, err := load(ctx, argument)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if digest, exists := seen[root.ID]; exists && digest != root.SHA256 {
			return nil, errors.New("conflicting face mask root digests")
		}
		if err := VerifyFaceMaskRoot(body, root, binding.Mask, source); err != nil {
			return nil, err
		}
		seen[root.ID] = root.SHA256
		result = append(result, ProposalFaceMaskRoot{Binding: binding, Root: root})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
