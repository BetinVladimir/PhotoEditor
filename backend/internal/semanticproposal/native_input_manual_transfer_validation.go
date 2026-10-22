package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) validateNativeInputManualTransferReference(ctx context.Context, who auth.Identity, request Request, configuration TransferPreparation, native nativeInputManualTransferReferenceDocuments) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := native.person
	if configuration.PersonProtection == nil || p.source.source.project != s.ProjectID || p.source.source.bucket != s.Bucket || configuration.SourceProtection != p.source.protection.verified.Root || configuration.PersonProtection.Report != p.person.report || !sameRegisteredNativeInputManualProtection(ctx, p.person.registered, p.source.protection) || !sameRegisteredNativeInputManualProtection(ctx, native.selection.registered, p.source.protection) {
		return v2input.ErrInvalid
	}
	if _, err := composeNativeInputManualSourceContinuation(ctx, who, request, p.source.source, p.source.protection, configuration.Color); err != nil {
		return err
	}
	if _, err := s.marshalNativeInputManualSelectionPins(who, request, native.selection); err != nil {
		return err
	}
	expected, err := canonical.Marshal(configuration.PersonProtection.Expected)
	if err != nil {
		return err
	}
	if string(expected) != p.person.expected {
		return v2input.ErrConflict
	}
	selected, err := selectNativeQualificationPolicies(p.policy, []string{"semantic.transfer", "mask.person"})
	if err != nil {
		return err
	}
	if !sameQualificationPolicyExpected(configuration.Expected, selected["semantic.transfer"]) || !sameQualificationPolicyExpected(configuration.PersonProtection.Expected, selected["mask.person"]) {
		return v2input.ErrConflict
	}
	refs := []v2input.Ref{request.Intent.Reference.Source, native.selection.root, p.source.protection.verified.Root}
	for i, raw := range [][]byte{native.reference, native.selected, native.protection} {
		sum := sha256.Sum256(raw)
		if len(raw) == 0 || hex.EncodeToString(sum[:]) != refs[i].SHA256 {
			return v2input.ErrConflict
		}
	}
	if err := configuration.Color.VerifyWorkingImage(native.reference); err != nil {
		return err
	}
	pixels, err := v2input.ResolveSemanticMaskPixels(native.selection.root, native.selected, request.Intent.Reference, native.reference, false)
	if err != nil {
		return err
	}
	protected, err := v2input.ResolveSemanticMaskPixels(configuration.SourceProtection, native.protection, request.Intent.Reference, native.reference, true)
	if err != nil {
		return err
	}
	if pixels != native.selection.pixels || protected != p.source.protection.verified.Pixels || string(native.protection) != string(p.source.protection.verified.Body) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
