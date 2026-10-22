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

func (s SQLTransferSourceReader) personQualificationObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected, report v2input.Ref) (personEvidenceObject, error) {
	empty := personEvidenceObject{}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || expected.Operation != "mask.person" || expected.Version != "1" {
		return empty, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return empty, err
	}
	var result personEvidenceObject
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_person_qualification_object($1,$2,$3,$4,$5)", tenantID, s.ProjectID, who.UserID, who.SessionID, body)
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

// ReadQualifiedPersonEvidence requires an independently operator-selected
// mask.person policy/report, then validates producer evidence and repeats the
// live imported qualification/object pin. Actual scientific measurements and
// final registration locks remain independent deployment obligations.
func (s SQLTransferSourceReader) ReadQualifiedPersonEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root, qualificationReport v2input.Ref, expected v2qualification.Expected) (VerifiedPersonMask, error) {
	expected = ownQualificationExpected(expected)
	empty := VerifiedPersonMask{}
	pin, err := s.personQualificationObject(ctx, who, tenantID, expected, qualificationReport)
	if err != nil {
		return empty, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
	if err != nil {
		return empty, err
	}
	if _, err := v2qualification.VerifyTyped(raw, pin.ref.SHA256, expected); err != nil {
		return empty, err
	}
	mask, err := s.ReadPersonEvidence(ctx, who, tenantID, binding, root, expected.ModelLockSHA, expected.Backend)
	if err != nil {
		return empty, err
	}
	if err := s.verifyPersonProducerProfile(ctx, who, tenantID, binding, mask, expected); err != nil {
		return empty, err
	}
	current, err := s.personQualificationObject(ctx, who, tenantID, expected, qualificationReport)
	if err != nil {
		return empty, err
	}
	if current != pin {
		return empty, v2input.ErrConflict
	}
 // The qualification lookup above may outlive a consent change or producer
 // invalidation. Recheck live producer authority after all evidence IO.
 if err := s.verifyPersonProducerProfile(ctx, who, tenantID, binding, mask, expected); err != nil {
  return empty, err
 }
 if err := ctx.Err(); err != nil { return empty, err }
	return mask, nil
}

// VerifyQualifiedPersonProtectionCoverage requires the independent person
// qualification throughout evidence/coverage IO. It does not select complete
// policy inventory or hold the final scheduler admission transaction locks.
func (s SQLTransferSourceReader) VerifyQualifiedPersonProtectionCoverage(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, personRoot, protectionRoot, qualificationReport v2input.Ref, expected v2qualification.Expected) (int64, error) {
	count, _, _, err := s.verifyQualifiedPersonProtectionCoveragePinned(ctx, who, tenantID, binding, personRoot, protectionRoot, qualificationReport, expected)
	return count, err
}

func (s SQLTransferSourceReader) verifyQualifiedPersonProtectionCoveragePinned(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, personRoot, protectionRoot, qualificationReport v2input.Ref, expected v2qualification.Expected) (int64, []personEvidenceObject, personEvidenceObject, error) {
	expected = ownQualificationExpected(expected)
	pin, err := s.personQualificationObject(ctx, who, tenantID, expected, qualificationReport)
	if err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
	if err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	if _, err := v2qualification.VerifyTyped(raw, pin.ref.SHA256, expected); err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	mask, err := s.ReadPersonMask(ctx, who, tenantID, binding, personRoot, expected.ModelLockSHA)
	if err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	if err := s.verifyPersonProducerProfile(ctx, who, tenantID, binding, mask, expected); err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	count, evidencePins, err := s.verifyPersonProtectionCoveragePinned(ctx, who, tenantID, binding, personRoot, protectionRoot, expected.ModelLockSHA, expected.Backend)
	if err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	current, err := s.personQualificationObject(ctx, who, tenantID, expected, qualificationReport)
	if err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	if current != pin {
		return 0, nil, personEvidenceObject{}, v2input.ErrConflict
	}
	if err := s.verifyPersonProducerProfile(ctx, who, tenantID, binding, mask, expected); err != nil {
		return 0, nil, personEvidenceObject{}, err
	}
	return count, evidencePins, pin, nil
}
