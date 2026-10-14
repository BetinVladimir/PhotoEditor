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

// Returns a unique live catalog candidate for a server-selected face profile.
// This does not grant scientific/report-content or producer authority. Existing
// preparation verification must read and validate the original report bytes.
func (s SQLTransferSourceReader) discoverFaceQualificationReport(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected) (v2input.Ref, error) {
	if expected.Operation != "face.detect" && expected.Operation != "face.landmarks" && expected.Operation != "face.parse" {
		return v2input.Ref{}, v2qualification.ErrUnqualified
	}
	return s.discoverQualificationReport(ctx, who, tenantID, expected)
}

func (s SQLTransferSourceReader) discoverDefectQualificationReport(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected) (v2input.Ref, error) {
	if expected.Operation != "defects.propose" {
		return v2input.Ref{}, v2qualification.ErrUnqualified
	}
	return s.discoverQualificationReport(ctx, who, tenantID, expected)
}

func (s SQLTransferSourceReader) discoverPersonQualificationReport(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected) (v2input.Ref, error) {
	if expected.Operation != "mask.person" {
		return v2input.Ref{}, v2qualification.ErrUnqualified
	}
	return s.discoverQualificationReport(ctx, who, tenantID, expected)
}

func (s SQLTransferSourceReader) discoverQualificationReport(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenantID == uuid.Nil || s.ProjectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || expected.Version != "1" || (expected.Operation != "face.detect" && expected.Operation != "face.landmarks" && expected.Operation != "face.parse" && expected.Operation != "defects.propose" && expected.Operation != "mask.person") {
		return zero, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(ownQualificationExpected(expected))
	if err != nil {
		return zero, err
	}
	query := "SELECT DISTINCT artifact_id,sha256 FROM photo.semantic_proposal_face_qualification_object($1,$2,$3,$4,$5) LIMIT 2"
	if expected.Operation == "defects.propose" {
		query = "SELECT artifact_id,sha256 FROM photo.semantic_proposal_defect_qualification_candidates($1,$2,$3,$4,$5)"
	}
	if expected.Operation == "mask.person" {
		query = "SELECT DISTINCT artifact_id,sha256 FROM photo.semantic_proposal_person_qualification_object($1,$2,$3,$4,$5) LIMIT 2"
	}
	var candidate v2input.Ref
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, query, tenantID, s.ProjectID, who.UserID, who.SessionID, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var ref v2input.Ref
			if err := rows.Scan(&ref.ArtifactID, &ref.SHA256); err != nil {
				return err
			}
			if ref.ArtifactID == uuid.Nil {
				return v2qualification.ErrUnqualified
			}
			if _, err := domain.ParseDigest(ref.SHA256); err != nil {
				return v2qualification.ErrUnqualified
			}
			count++
			candidate = ref
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 1 {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return candidate, nil
}
