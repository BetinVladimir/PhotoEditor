package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Exact original parent provenance, never an arbitrary caller-selected root.
// The coverage reader independently repeats qualification, producer evidence,
// object versions and pixel coverage. This selector grants no SQL admission.
func (s SQLTransferSourceReader) nativeInputCoverageCandidate(ctx context.Context, who auth.Identity, request Request, registered registeredNativeInputProtection, report v2input.Ref, expected v2qualification.Expected, originalPinned bool) (personCandidate, error) {
	if !originalPinned {
		return s.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, expected)
	}
	var zero personCandidate
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	plan := registered.published.preparation.plan
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	p := plan.prepared.dependencies.preparation.person
	if p == nil {
		return zero, v2input.ErrInvalid
	}
	if err := s.validateNativePersonInputAuthority(ctx, who, request, *p); err != nil {
		return zero, err
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	if p.report != report || p.expected != string(body) {
		return zero, v2input.ErrConflict
	}
	return personCandidate{root: p.root, pixels: p.pixels}, nil
}
