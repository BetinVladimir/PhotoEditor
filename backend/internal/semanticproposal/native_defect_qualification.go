package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Deployment-selected report refs. No newest-report or caller-mask fallback.
type nativeDefectQualificationReports struct {
	Defect        v2input.Ref
	Faces, Parser [2]v2input.Ref
}

type nativeDefectQualificationProof struct {
	verified               bool
	faceSet, parserSet     string
	faceCount, parserCount int
	parent                 nativeDefectParentProof
	policy                 qualificationPolicySnapshot
	defect                 defectQualificationPreparationProof
	reports                nativeFaceReportProof
	faces                  []facePreparationProof
	parsers                []faceParsePreparationProof
}

// Independently qualifies the exact native Params' landmark/parser producers.
// This does not admit transfer, qualify targets, or activate the parent graph.
func (s SQLTransferSourceReader) prepareQualifiedNativeDefectResult(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, reports nativeDefectQualificationReports, color TransferColorProfile) (nativeDefectQualificationProof, error) {
	var zero nativeDefectQualificationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	expected, err := selectNativeQualificationPolicies(policy, []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"})
	if err != nil {
		return zero, err
	}
	native, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, err
	}
	policySHA, err := canonical.Hash(json.RawMessage(policy.body))
	if err != nil {
		return zero, err
	}
	if err := s.nativePolicyReady(ctx, who, request, native.parentProof, policySHA); err != nil {
		return zero, err
	}
	defect, err := s.readDefectQualificationPreparation(ctx, who, request.TenantID, reports.Defect, expected["defects.propose"])
	if err != nil {
		return zero, err
	}
	retainedReports, err := s.readNativeFaceReports(ctx, who, request.TenantID, reports, expected)
	if err != nil {
		return zero, err
	}
	_, faces, err := s.readQualifiedDefectFaceEvidence(ctx, who, request.TenantID, request.Intent.Reference, native.Pins, reports.Faces, [2]v2qualification.Expected{expected["face.landmarks"], expected["face.detect"]})
	if err != nil {
		return zero, err
	}
	_, parsers, err := s.readQualifiedDefectFaceMaskEvidence(ctx, who, request.TenantID, request.Intent.Reference, native.Pins, reports.Parser, [2]v2qualification.Expected{expected["face.parse"], expected["face.detect"]}, color)
	if err != nil {
		return zero, err
	}
	// Later dependency IO must not replace the retained native canonical result.
	current, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, err
	}
	if current.parentProof != native.parentProof {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, request.TenantID, defect, expected["defects.propose"]); err != nil {
		return zero, err
	}
	for _, proof := range faces {
		if err := s.recheckFacePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, proof); err != nil {
			return zero, err
		}
	}
	for _, proof := range parsers {
		if err := s.recheckFaceParsePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, proof); err != nil {
			return zero, err
		}
	}
	if err := s.recheckNativeFaceReports(ctx, who, request.TenantID, retainedReports, expected); err != nil {
		return zero, err
	}
	if err := matchNativeFaceProducerReports(retainedReports, faces, parsers); err != nil {
		return zero, err
	}
	currentPolicy, err := s.readQualificationPolicySnapshot(ctx, who, request.TenantID)
	if err != nil {
		return zero, err
	}
	if currentPolicy != policy {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	faceSet, parserSet, err := nativeQualifiedProducerSets(faces, parsers)
	if err != nil {
		return zero, err
	}
	return nativeDefectQualificationProof{verified: true, reports: retainedReports, faceSet: faceSet, parserSet: parserSet, faceCount: len(faces), parserCount: len(parsers), parent: native.parentProof, policy: policy, defect: defect, faces: append([]facePreparationProof(nil), faces...), parsers: append([]faceParsePreparationProof(nil), parsers...)}, nil
}

// Caller composes source/person/selection/target and complete graph admission.
// A refusal after any component requires full TX rollback. No object IO here.
func (s SQLTransferSourceReader) lockQualifiedNativeDefectResult(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeDefectQualificationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !proof.verified || len(proof.faces) != proof.faceCount || len(proof.parsers) != proof.parserCount || !validQualificationPolicySnapshot(s, who, request.TenantID, proof.policy) {
		return v2input.ErrInvalid
	}
	faceSet, parserSet, err := nativeQualifiedProducerSets(proof.faces, proof.parsers)
	if err != nil || faceSet != proof.faceSet || parserSet != proof.parserSet {
		return v2input.ErrInvalid
	}
	expected, err := selectNativeQualificationPolicies(proof.policy, []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"})
	if err != nil {
		return err
	}
	if _, err := s.marshalNativeFaceReportPins(who, request.TenantID, proof.reports, expected); err != nil {
		return err
	}
	if err := matchNativeFaceProducerReports(proof.reports, proof.faces, proof.parsers); err != nil {
		return err
	}
	match := func(body string, ops [2]string) bool {
		var values [2]v2qualification.Expected
		if json.Unmarshal([]byte(body), &values) != nil {
			return false
		}
		for i, value := range values {
			if value.Operation != ops[i] || !sameQualificationPolicyExpected(value, expected[ops[i]]) {
				return false
			}
		}
		return true
	}
	for _, face := range proof.faces {
		if !match(face.expected, [2]string{"face.landmarks", "face.detect"}) {
			return v2qualification.ErrUnqualified
		}
	}
	for _, parser := range proof.parsers {
		if !match(parser.evidence.expected, [2]string{"face.parse", "face.detect"}) {
			return v2qualification.ErrUnqualified
		}
	}
	if err := s.lockNativeDefectContinuation(ctx, tx, who, request.TenantID, request.Intent.Reference, proof.parent); err != nil {
		return err
	}
	if err := s.lockDefectQualificationPreparation(ctx, tx, who, request.TenantID, proof.defect, expected["defects.propose"]); err != nil {
		return err
	}
	if err := s.lockNativeFaceReports(ctx, tx, who, request.TenantID, proof.reports, expected); err != nil {
		return err
	}
	if len(proof.faces) > 0 {
		if err := s.lockFacePreparationProofs(ctx, tx, who, request.TenantID, request.Intent.Reference, proof.faces); err != nil {
			return err
		}
	}
	if len(proof.parsers) > 0 {
		if err := s.lockFaceParsePreparationProofs(ctx, tx, who, request.TenantID, request.Intent.Reference, proof.parsers); err != nil {
			return err
		}
	}
	sha, err := canonical.Hash(json.RawMessage(proof.policy.body))
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_native_policy($1,$2,$3,$4,$5,$6::jsonb,$7,$8::jsonb)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, proof.parent.result.task, proof.parent.anchor, sha, proof.policy.body).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
