package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Prepares reference-side native transfer authority with actual pinned source
// pixels/ICC, declared native producers and independently qualified protection.
// No draft, graph registration, quota reservation or activation occurs here.
func (s SQLTransferSourceReader) prepareNativeSourceContinuation(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root v2input.Ref, reports nativeDefectQualificationReports, person TransferPersonProtection, color TransferColorProfile) (v2input.SemanticSourceMetadata, nativeSourceContinuationProof, error) {
	fail := func(err error) (v2input.SemanticSourceMetadata, nativeSourceContinuationProof, error) {
		return v2input.SemanticSourceMetadata{}, nativeSourceContinuationProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || who.UserID != request.ActorID || request.TenantID == uuid.Nil || s.ProjectID == uuid.Nil || task == uuid.Nil {
		return fail(v2input.ErrInvalid)
	}
	person.Expected = ownQualificationExpected(person.Expected)
	// Original parent authorization precedes source object IO.
	original, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return fail(err)
	}
	metadata, source, err := s.readQualifiedSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, color, false)
	if err != nil {
		return fail(err)
	}
	defect, err := s.prepareQualifiedNativeDefectResult(ctx, who, request, key, intent, snapshot, task, reports, color)
	if err != nil {
		return fail(err)
	}
	protection, err := s.prepareNativeReferenceProtection(ctx, who, request, key, intent, snapshot, task, root, person.Report, person.Expected)
	if err != nil {
		return fail(err)
	}
	native, err := composeNativeReferenceContinuation(defect, protection)
	if err != nil {
		return fail(err)
	}
	if native.defect.parent != original.parentProof {
		return fail(v2input.ErrConflict)
	}
	// Later native/person dependency IO cannot replace retained source metadata.
	if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, color, false, source); err != nil {
		return fail(err)
	}
	selected, err := selectNativeQualificationPolicies(native.defect.policy, []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"})
	if err != nil {
		return fail(err)
	}
	if err := s.recheckNativeFaceReports(ctx, who, request.TenantID, native.defect.reports, selected); err != nil {
		return fail(err)
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, request.TenantID, native.defect.defect, selected["defects.propose"]); err != nil {
		return fail(err)
	}
	for _, face := range native.defect.faces {
		if err := s.recheckFacePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, face); err != nil {
			return fail(err)
		}
	}
	for _, parser := range native.defect.parsers {
		if err := s.recheckFaceParsePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, parser); err != nil {
			return fail(err)
		}
	}
	current, err := s.nativeDefectResultCatalog(ctx, who, request.TenantID, request.Intent.Reference, task)
	if err != nil {
		return fail(err)
	}
	if current != native.defect.parent.result.catalog {
		return fail(v2input.ErrConflict)
	}
	if err := s.nativePolicyReady(ctx, who, request, native.defect.parent, native.protection.policySHA); err != nil {
		return fail(err)
	}
	currentPolicy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return fail(err)
	}
	if currentPolicy != native.defect.policy {
		return fail(v2input.ErrConflict)
	}
	proof, err := composeNativeSourceContinuation(source, native, color)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return metadata, proof, nil
}
