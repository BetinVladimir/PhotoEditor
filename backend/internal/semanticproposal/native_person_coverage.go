package semanticproposal

import (
	"context"
	"github.com/google/uuid"
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
type nativePersonProtectionProof struct {
	registered                        registeredNativeProtectionProof
	root, pixels                      v2input.Ref
	expected, evidence, qualification string
	count                             int64
}

func (s SQLTransferSourceReader) VerifyNativePersonProtectionCoverage(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root, qualificationReport v2input.Ref, expected v2qualification.Expected) (v2input.Ref, int64, error) {
	root, count, _, err := s.verifyNativePersonProtectionCoverage(ctx, who, request, key, intent, snapshot, task, root, qualificationReport, expected)
	return root, count, err
}

func (s SQLTransferSourceReader) verifyNativePersonProtectionCoverage(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root, qualificationReport v2input.Ref, expected v2qualification.Expected) (v2input.Ref, int64, nativePersonProtectionProof, error) {
	expected = ownQualificationExpected(expected)
	selected, err := s.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, expected)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	qualification, err := s.personQualificationObject(ctx, who, request.TenantID, expected, qualificationReport)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	retainedPerson, err := s.ReadPersonMask(ctx, who, request.TenantID, request.Intent.Reference, selected.root, expected.ModelLockSHA)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	evidence, err := s.personEvidenceObjects(ctx, who, request.TenantID, request.Intent.Reference, retainedPerson)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	person, err := s.ReadQualifiedPersonEvidence(ctx, who, request.TenantID, request.Intent.Reference, selected.root, qualificationReport, expected)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	if person.Pixels != selected.pixels {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	if person.Root != retainedPerson.Root || person.Pixels != retainedPerson.Pixels || string(person.Body) != string(retainedPerson.Body) {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	protection, proof, err := s.readRegisteredNativeDefectProtection(ctx, who, request, key, intent, snapshot, task, root)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	node := v2draft.LayerRef{Ref: person.Pixels, Contract: "MaskPixels", ParentRef: person.Root}
	requiredObject, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	left, err := s.Objects.Open(ctx, requiredObject)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	protectedObject := proof.objects[1].object
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		_ = left.Close()
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	count, verifyErr := masknpy.VerifyMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: requiredObject.Bytes, SHA256: person.Pixels.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr = classifyPersonCoverageError(verifyErr); verifyErr != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, verifyErr
	}
	if leftErr != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, leftErr
	}
	if rightErr != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, rightErr
	}
	if err := s.VerifyPersonMaskOrigin(ctx, who, request.TenantID, request.Intent.Reference, person.Root, person.Pixels); err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	currentEvidence, err := s.personEvidenceObjects(ctx, who, request.TenantID, request.Intent.Reference, person)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	if len(currentEvidence) != len(evidence) {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	for i, pin := range evidence {
		if currentEvidence[i] != pin {
			return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
		}
	}
	currentQualification, err := s.personQualificationObject(ctx, who, request.TenantID, expected, qualificationReport)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	if currentQualification != qualification {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	currentCandidate, err := s.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, expected)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	if currentCandidate != selected {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	currentObject, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	if currentObject != requiredObject {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	currentProtection, err := s.registeredNativeProtectionObjects(ctx, who, request, task, root, proof.parent)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	if currentProtection != proof.objects {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	evidenceBody, err := marshalPersonEvidencePins(evidence, s.Bucket)
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	qualificationBody, err := canonical.Marshal(personEvidencePin{qualification.contract, qualification.ref.ArtifactID, qualification.ref.SHA256, s.Bucket, qualification.object.Key, qualification.object.VersionID, qualification.object.Bytes, qualification.backend})
	if err != nil {
		return v2input.Ref{}, 0, nativePersonProtectionProof{}, err
	}
	retained := nativePersonProtectionProof{registered: proof, root: person.Root, pixels: person.Pixels, expected: string(expectedBody), evidence: string(evidenceBody), qualification: string(qualificationBody), count: count}
	return person.Root, count, retained, nil
}
