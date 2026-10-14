package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeTransferReferenceDocuments struct {
	reference, selected, protection json.RawMessage
	proof                           nativeSourceContinuationProof
}

// Reads the exact three compiler documents retained by original native source
// authority. Generic protection provenance is neither inferred nor broadened.
func (s SQLTransferSourceReader) readNativeTransferReferenceDocuments(ctx context.Context, who auth.Identity, request Request, proof nativeSourceContinuationProof, color TransferColorProfile, budget int64) (nativeTransferReferenceDocuments, error) {
	var zero nativeTransferReferenceDocuments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || budget <= 0 || budget > 9007199254740991 || proof.source.tenant != request.TenantID || proof.source.actor != who.UserID || proof.source.actor != request.ActorID || proof.source.session != who.SessionID || proof.source.project != s.ProjectID || proof.source.bucket != s.Bucket || proof.source.binding != request.Intent.Reference {
		return zero, v2input.ErrInvalid
	}
	owned, err := composeNativeSourceContinuation(proof.source, proof.native, color)
	if err != nil {
		return zero, err
	}
	selection := owned.native.protection.selection
	registered := selection.registered
	if selection.root != request.Intent.ReferenceMask {
		return zero, v2input.ErrConflict
	}
	if err := s.nativePolicyReady(ctx, who, request, registered.parent, owned.native.protection.policySHA); err != nil {
		return zero, err
	}
	beforePolicy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	if beforePolicy != owned.native.defect.policy {
		return zero, v2input.ErrConflict
	}
	selected, err := selectQualificationPolicies(beforePolicy, []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"})
	if err != nil {
		return zero, err
	}
	if err := s.recheckNativeFaceReports(ctx, who, request.TenantID, owned.native.defect.reports, selected); err != nil {
		return zero, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, request.TenantID, owned.native.defect.defect, selected["defects.propose"]); err != nil {
		return zero, err
	}
	sourcePins, err := decodeSourcePreparationPins(owned.source)
	if err != nil {
		return zero, err
	}
	objects := []personEvidenceObject{
		{ref: request.Intent.Reference.Source, object: sourcePins[0].Object},
		{ref: selection.root, object: selection.rootObject},
		{ref: registered.objects[0].ref, object: registered.objects[0].object},
	}
	if _, err := nativeTransferDocumentBudget(objects, budget); err != nil {
		return zero, err
	}
	current, err := s.registeredNativeProtectionObjects(ctx, who, request, registered.parent.result.task, registered.objects[0].ref, registered.parent)
	if err != nil {
		return zero, err
	}
	if current != registered.objects {
		return zero, v2input.ErrConflict
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	selectedObject, err := catalog.Lookup(ctx, who, request.TenantID, v2draft.LayerRef{Ref: selection.root, Contract: "MaskRef"})
	if err != nil {
		return zero, err
	}
	if selectedObject != selection.rootObject {
		return zero, v2input.ErrConflict
	}
	var bodies [3]json.RawMessage
	for i, pin := range objects {
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return zero, err
		}
		bodies[i] = raw
	}
	if err := color.VerifyWorkingImage(bodies[0]); err != nil {
		return zero, err
	}
	selectedPixels, err := v2input.ResolveSemanticMaskPixels(selection.root, bodies[1], request.Intent.Reference, bodies[0], false)
	if err != nil {
		return zero, err
	}
	if selectedPixels != selection.pixels {
		return zero, v2input.ErrConflict
	}
	protectionPixels, err := v2input.ResolveSemanticMaskPixels(registered.objects[0].ref, bodies[2], request.Intent.Reference, bodies[0], true)
	if err != nil {
		return zero, err
	}
	if protectionPixels != registered.objects[1].ref {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, color, false, owned.source); err != nil {
		return zero, err
	}
	current, err = s.registeredNativeProtectionObjects(ctx, who, request, registered.parent.result.task, registered.objects[0].ref, registered.parent)
	if err != nil {
		return zero, err
	}
	if current != registered.objects {
		return zero, v2input.ErrConflict
	}
	selectedObject, err = catalog.Lookup(ctx, who, request.TenantID, v2draft.LayerRef{Ref: selection.root, Contract: "MaskRef"})
	if err != nil {
		return zero, err
	}
	if selectedObject != selection.rootObject {
		return zero, v2input.ErrConflict
	}
	selectedPixelsObject, err := catalog.Lookup(ctx, who, request.TenantID, v2draft.LayerRef{Ref: selection.pixels, Contract: "MaskPixels", ParentRef: selection.root})
	if err != nil {
		return zero, err
	}
	if selectedPixelsObject != selection.pixelsObject {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativeFaceReports(ctx, who, request.TenantID, owned.native.defect.reports, selected); err != nil {
		return zero, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, request.TenantID, owned.native.defect.defect, selected["defects.propose"]); err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	if policy != owned.native.defect.policy {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeTransferReferenceDocuments{reference: bodies[0], selected: bodies[1], protection: bodies[2], proof: owned}, nil
}
