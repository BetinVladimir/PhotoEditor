package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Discovers one independently qualified person producer and requires positive
// person support to be covered by the exact-parent registered native protection.
// Policy inventory stability and final graph admission require separate TX locks.
type nativeInputManualPersonProtectionProof struct {
	registered                        registeredNativeInputManualInitialProtection
	root, pixels                      v2input.Ref
	expected, evidence, qualification string
	count                             int64
	report                            v2input.Ref
}

func (s SQLTransferSourceReader) verifyNativeInputManualPersonProtectionCoverage(ctx context.Context, who auth.Identity, request Request, registered registeredNativeInputManualInitialProtection, color TransferColorProfile, qualificationReport v2input.Ref, expected v2qualification.Expected) (v2input.Ref, int64, nativeInputManualPersonProtectionProof, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	expected = ownQualificationExpected(expected)
	selected, err := s.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, expected)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	qualification, err := s.personQualificationObject(ctx, who, request.TenantID, expected, qualificationReport)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	retainedPerson, err := s.ReadPersonMask(ctx, who, request.TenantID, request.Intent.Reference, selected.root, expected.ModelLockSHA)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	evidence, err := s.personEvidenceObjects(ctx, who, request.TenantID, request.Intent.Reference, retainedPerson)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	person, err := s.ReadQualifiedPersonEvidence(ctx, who, request.TenantID, request.Intent.Reference, selected.root, qualificationReport, expected)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if person.Pixels != selected.pixels {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	if person.Root != retainedPerson.Root || person.Pixels != retainedPerson.Pixels || string(person.Body) != string(retainedPerson.Body) {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	proof, err := s.readRegisteredNativeInputManualInitialProtection(ctx, who, registered.published, color)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if !sameRegisteredNativeInputManualProtection(ctx, proof, registered) {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	protection := proof.verified
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	node := v2draft.LayerRef{Ref: person.Pixels, Contract: "MaskPixels", ParentRef: person.Root}
	requiredObject, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	left, err := s.Objects.Open(ctx, requiredObject)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	protectedObject := proof.objects[1].object
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		_ = left.Close()
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	count, verifyErr := masknpy.VerifyMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: requiredObject.Bytes, SHA256: person.Pixels.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr = classifyPersonCoverageError(verifyErr); verifyErr != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, verifyErr
	}
	if leftErr != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, leftErr
	}
	if rightErr != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, rightErr
	}
	if err := s.VerifyPersonMaskOrigin(ctx, who, request.TenantID, request.Intent.Reference, person.Root, person.Pixels); err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	currentEvidence, err := s.personEvidenceObjects(ctx, who, request.TenantID, request.Intent.Reference, person)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if len(currentEvidence) != len(evidence) {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	for i, pin := range evidence {
		if currentEvidence[i] != pin {
			return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
		}
	}
	currentQualification, err := s.personQualificationObject(ctx, who, request.TenantID, expected, qualificationReport)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if currentQualification != qualification {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	currentCandidate, err := s.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, expected)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if currentCandidate != selected {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	currentObject, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if currentObject != requiredObject {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	currentProtection, err := s.readRegisteredNativeInputManualInitialProtection(ctx, who, registered.published, color)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	if !sameRegisteredNativeInputManualProtection(ctx, currentProtection, proof) {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	evidenceBody, err := marshalPersonEvidencePins(evidence, s.Bucket)
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	qualificationBody, err := canonical.Marshal(personEvidencePin{qualification.contract, qualification.ref.ArtifactID, qualification.ref.SHA256, s.Bucket, qualification.object.Key, qualification.object.VersionID, qualification.object.Bytes, qualification.backend})
	if err != nil {
		return v2input.Ref{}, 0, nativeInputManualPersonProtectionProof{}, err
	}
	retained := nativeInputManualPersonProtectionProof{registered: proof, root: person.Root, pixels: person.Pixels, expected: string(expectedBody), evidence: string(evidenceBody), qualification: string(qualificationBody), count: count, report: qualificationReport}
	return person.Root, count, retained, nil
}
