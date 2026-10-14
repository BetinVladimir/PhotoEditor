package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Qualification evidence is retained even when the native Params declare zero
// producers for a family. Report pins do not fabricate producer evidence.
type nativeFaceReportProof struct {
	tenant, project, actor, session uuid.UUID
	bucket                          string
	expected                        [3]string
	reports                         [3]personEvidenceObject
	originalReports                 [3]personEvidenceObject
}

var nativeFaceReportOperations = [3]string{"face.landmarks", "face.detect", "face.parse"}

func (s SQLTransferSourceReader) readNativeFaceReports(ctx context.Context, who auth.Identity, tenant uuid.UUID, reports nativeDefectQualificationReports, selected map[string]v2qualification.Expected) (nativeFaceReportProof, error) {
	var zero nativeFaceReportProof
	if reports.Faces[1] != reports.Parser[1] {
		return zero, v2input.ErrConflict
	}
	proof := nativeFaceReportProof{tenant: tenant, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket}
	refs := [3]v2input.Ref{reports.Faces[0], reports.Faces[1], reports.Parser[0]}
	for i, op := range nativeFaceReportOperations {
		expected := ownQualificationExpected(selected[op])
		if expected.Operation != op {
			return zero, v2qualification.ErrUnqualified
		}
		if _, err := s.VerifyFaceQualification(ctx, who, tenant, refs[i], expected); err != nil {
			return zero, err
		}
		pin, err := s.faceQualificationObject(ctx, who, tenant, expected, refs[i])
		if err != nil {
			return zero, err
		}
		raw, err := canonical.Marshal(expected)
		if err != nil {
			return zero, err
		}
		proof.expected[i], proof.reports[i] = string(raw), pin
		proof.originalReports[i] = pin
	}
	if err := s.recheckNativeFaceReports(ctx, who, tenant, proof, selected); err != nil {
		return zero, err
	}
	return proof, nil
}

func (s SQLTransferSourceReader) marshalNativeFaceReportPins(who auth.Identity, tenant uuid.UUID, proof nativeFaceReportProof, selected map[string]v2qualification.Expected) ([3]string, error) {
	var pins [3]string
	if tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || s.Bucket == "" || proof.tenant != tenant || proof.project != s.ProjectID || proof.actor != who.UserID || proof.session != who.SessionID || proof.bucket != s.Bucket {
		return pins, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for i, op := range nativeFaceReportOperations {
		var expected v2qualification.Expected
		if len(proof.expected[i]) > 4<<20 || json.Unmarshal([]byte(proof.expected[i]), &expected) != nil || expected.Operation != op || expected.Version != "1" || !sameQualificationPolicyExpected(expected, selected[op]) {
			return [3]string{}, v2qualification.ErrUnqualified
		}
		body, err := canonical.Marshal(expected)
		if err != nil || string(body) != proof.expected[i] {
			return [3]string{}, v2input.ErrInvalid
		}
		q := proof.reports[i]
		if q != proof.originalReports[i] {
			return [3]string{}, v2input.ErrConflict
		}
		if q.contract != "QualificationReport" || q.backend != expected.Backend || q.ref.ArtifactID == uuid.Nil || seen[q.ref.ArtifactID] || q.object.SHA256 != q.ref.SHA256 || q.object.ContentType != "application/json" || q.object.Key == "" || q.object.VersionID == "" || q.object.VersionID == "null" || q.object.Bytes < 2 || q.object.Bytes > 8<<20 {
			return [3]string{}, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(q.ref.SHA256); err != nil {
			return [3]string{}, v2input.ErrInvalid
		}
		seen[q.ref.ArtifactID] = true
		raw, err := canonical.Marshal(personEvidencePin{q.contract, q.ref.ArtifactID, q.ref.SHA256, proof.bucket, q.object.Key, q.object.VersionID, q.object.Bytes, q.backend})
		if err != nil || len(raw) > 16384 {
			return [3]string{}, v2input.ErrInvalid
		}
		pins[i] = string(raw)
	}
	return pins, nil
}

func (s SQLTransferSourceReader) recheckNativeFaceReports(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof nativeFaceReportProof, selected map[string]v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := s.marshalNativeFaceReportPins(who, tenant, proof, selected); err != nil {
		return err
	}
	for i, op := range nativeFaceReportOperations {
		pin, err := s.faceQualificationObject(ctx, who, tenant, selected[op], proof.reports[i].ref)
		if err != nil {
			return err
		}
		if pin != proof.reports[i] {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) lockNativeFaceReports(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, proof nativeFaceReportProof, selected map[string]v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	pins, err := s.marshalNativeFaceReportPins(who, tenant, proof, selected)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	for i, op := range nativeFaceReportOperations {
		if err := ctx.Err(); err != nil {
			return err
		}
		query := "SELECT photo.lock_semantic_proposal_face_qualification($1,$2,$3,$4,$5,$6)"
		if op == "face.parse" {
			query = "SELECT photo.lock_semantic_proposal_face_parse_qualification($1,$2,$3,$4,$5,$6)"
		}
		var ready bool
		if err := tx.QueryRow(ctx, query, tenant, s.ProjectID, who.UserID, who.SessionID, proof.expected[i], pins[i]).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}
