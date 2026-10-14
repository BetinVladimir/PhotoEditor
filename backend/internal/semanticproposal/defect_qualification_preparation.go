package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (s SQLTransferSourceReader) defectQualificationObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected, report v2input.Ref) (personEvidenceObject, error) {
	empty := personEvidenceObject{}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || expected.Operation != "defects.propose" || expected.Version != "1" {
		return empty, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return empty, err
	}
	reportBody, err := canonical.Marshal(report)
	if err != nil {
		return empty, err
	}
	var result personEvidenceObject
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_qualification_object($1,$2,$3,$4,$5,$6)", tenantID, s.ProjectID, who.UserID, who.SessionID, body, reportBody)
		if err != nil {
			return err
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var e personEvidenceObject
			var bucket string
			if err := rows.Scan(&e.ref.ArtifactID, &e.ref.SHA256, &bucket, &e.object.Key, &e.object.VersionID, &e.object.Bytes); err != nil {
				return err
			}
			if e.ref != report {
				continue
			}
			if found || bucket != s.Bucket {
				return v2qualification.ErrUnqualified
			}
			e.object.SHA256 = e.ref.SHA256
			e.object.ContentType = "application/json"
			e.contract = "QualificationReport"
			e.backend = expected.Backend
			result = e
			found = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !found {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	return result, nil
}

// Retained metadata follows original-byte verification and repeated live lookup.
// This is not a transaction lock or queue admission authority.
type defectQualificationPreparationProof struct {
	tenant, project, actor, session uuid.UUID
	bucket, expected                string
	report                          personEvidenceObject
	metrics                         v2qualification.Metrics
}

func (s SQLTransferSourceReader) readDefectQualificationPreparation(ctx context.Context, who auth.Identity, tenant uuid.UUID, report v2input.Ref, expected v2qualification.Expected) (defectQualificationPreparationProof, error) {
	var zero defectQualificationPreparationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	if expected.Operation != "defects.propose" || expected.Version != "1" {
		return zero, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(expected)
	if err != nil || len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	pin, err := s.defectQualificationObject(ctx, who, tenant, expected, report)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
	if err != nil {
		return zero, err
	}
	verified, err := v2qualification.Verify(raw, pin.ref.SHA256, expected)
	if err != nil {
		return zero, err
	}
	current, err := s.defectQualificationObject(ctx, who, tenant, expected, report)
	if err != nil {
		return zero, err
	}
	if current != pin {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return defectQualificationPreparationProof{tenant: tenant, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, expected: string(body), report: pin, metrics: verified.Metrics}, nil
}

// readDefectExecutionQualification binds installed additional resource costs to
// verified original report peaks. It does not estimate image-dependent overhead
// or grant queued admission; those remain separate compiler obligations.
func (s SQLTransferSourceReader) readDefectExecutionQualification(ctx context.Context, who auth.Identity, tenant uuid.UUID, report v2input.Ref, expected v2qualification.Expected, costs v2input.SemanticQualifiedCosts) (defectQualificationPreparationProof, error) {
	var zero defectQualificationPreparationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := validateTransferMeasuredCosts(costs, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	proof, err := s.readDefectQualificationPreparation(ctx, who, tenant, report, expected)
	if err != nil {
		return zero, err
	}
	if err := validateTransferMeasuredCosts(costs, proof.metrics); err != nil {
		return zero, err
	}
	return proof, nil
}

// recheckDefectQualificationPreparation repeats live imported policy/report
// authority after later preparation IO. It never substitutes for final SQL locks.
func (s SQLTransferSourceReader) recheckDefectQualificationPreparation(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof defectQualificationPreparationProof, expected v2qualification.Expected) error {
	expected = ownQualificationExpected(expected)
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, proof, expected); err != nil {
		return err
	}
	current, err := s.defectQualificationObject(ctx, who, tenant, expected, proof.report.ref)
	if err != nil {
		return err
	}
	if current != proof.report {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
