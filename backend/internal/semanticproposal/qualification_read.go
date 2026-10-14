package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type qualificationObject struct {
	contract string
	ref      v2input.Ref
	object   objectstore.Object
}

func (s SQLTransferSourceReader) qualificationObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, pin v2input.SemanticQualificationPin) ([]qualificationObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.ProjectID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(pin)
	if err != nil {
		return nil, err
	}
	var result []qualificationObject
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT contract,artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_qualification_objects($1,$2,$3,$4,$5)", tenantID, s.ProjectID, who.UserID, who.SessionID, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var entry qualificationObject
			var bucket string
			if err := rows.Scan(&entry.contract, &entry.ref.ArtifactID, &entry.ref.SHA256, &bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes); err != nil {
				return err
			}
			expected := pin.Report
			if entry.contract == "Calibration" {
				expected = pin.Calibration
			} else if entry.contract != "Report" {
				return v2input.ErrInvalid
			}
			if len(result) >= 2 || bucket != s.Bucket || entry.ref != expected {
				return v2input.ErrInvalid
			}
			entry.object.SHA256 = entry.ref.SHA256
			entry.object.ContentType = "application/json"
			result = append(result, entry)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result) != 2 || result[0].contract != "Calibration" || result[1].contract != "Report" {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// transferQualificationPreparationProof retains exact metadata only after both
// original documents and a repeated authorized catalog lookup have passed.
// It does not hold transaction locks or confer admission authority.
type transferQualificationPreparationProof struct {
	tenant, project, actor, session uuid.UUID
	bucket                          string
	pin                             v2input.SemanticQualificationPin
	expected                        string
	objects                         [2]qualificationObject
	metrics                         v2qualification.Metrics
}

// VerifyQualification preserves the public verification contract while the
// private preparer also retains evidence for the subsequent transaction gate.
func (s SQLTransferSourceReader) VerifyQualification(ctx context.Context, who auth.Identity, tenantID uuid.UUID, pin v2input.SemanticQualificationPin, expected v2qualification.Expected) error {
	_, err := s.readTransferQualificationPreparation(ctx, who, tenantID, pin, expected)
	return err
}

func (s SQLTransferSourceReader) readTransferQualificationPreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, pin v2input.SemanticQualificationPin, expected v2qualification.Expected) (transferQualificationPreparationProof, error) {
	var zero transferQualificationPreparationProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected = ownQualificationExpected(expected)
	if tenantID == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyQualificationConfiguration(pin, expected); err != nil {
		return zero, err
	}
	body, err := canonical.Marshal(expected)
	if err != nil || len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	objects, err := s.qualificationObjects(ctx, who, tenantID, pin)
	if err != nil {
		return zero, err
	}
	calibration, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[0].object, objects[0].ref)
	if err != nil {
		return zero, err
	}
	report, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[1].object, objects[1].ref)
	if err != nil {
		return zero, err
	}
	verified, err := verifyQualificationDocuments(pin, expected, report, calibration)
	if err != nil {
		return zero, err
	}
	current, err := s.qualificationObjects(ctx, who, tenantID, pin)
	if err != nil {
		return zero, err
	}
	if current[0] != objects[0] || current[1] != objects[1] {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return transferQualificationPreparationProof{
		tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID,
		bucket: s.Bucket, pin: pin, expected: string(body), objects: [2]qualificationObject{objects[0], objects[1]}, metrics: verified.Metrics,
	}, nil
}
