package v2optical

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveDetectedLinesProposal struct {
	SchemaVersion int                      `json:"schema_version"`
	Tenant        uuid.UUID                `json:"tenant_id"`
	Project       uuid.UUID                `json:"project_id"`
	Producer      uuid.UUID                `json:"producer_task_id"`
	ResultID      uuid.UUID                `json:"result_id"`
	Result        v2input.Ref              `json:"result"`
	Detection     DetectedPerspectiveLines `json:"detection"`
}
type PerspectiveDetectedLinesService struct{ DB *pgxpool.Pool }

func (s PerspectiveDetectedLinesService) Describe(ctx context.Context, who auth.Identity, tenantID, project, version, task uuid.UUID) (PerspectiveDetectedLinesProposal, error) {
	var zero PerspectiveDetectedLinesProposal
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || project == uuid.Nil || version == uuid.Nil || task == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var resultID uuid.UUID
	var root v2input.Ref
	var raw string
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return v2input.ErrInvalid
		}
		err := tx.QueryRow(ctx, "SELECT result_id,artifact_id,sha256,result_jcs FROM photo.v29_adopted_perspective_lines($1,$2,$3,$4,$5,$6)", tenantID, project, version, task, who.UserID, who.SessionID).Scan(&resultID, &root.ArtifactID, &root.SHA256, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return v2input.ErrExpired
		}
		return err
	})
	if err != nil {
		return zero, err
	}
	return describePerspectiveDetectedLines(tenantID, project, version, task, resultID, root, []byte(raw))
}
func describePerspectiveDetectedLines(tenantID, project, version, task, resultID uuid.UUID, root v2input.Ref, raw []byte) (PerspectiveDetectedLinesProposal, error) {
	if tenantID == uuid.Nil || project == uuid.Nil || version == uuid.Nil || task == uuid.Nil || resultID == uuid.Nil {
		return PerspectiveDetectedLinesProposal{}, v2input.ErrInvalid
	}
	detection, err := ParseDetectedPerspectiveLines(raw, root, version.String())
	if err != nil {
		return PerspectiveDetectedLinesProposal{}, err
	}
	return PerspectiveDetectedLinesProposal{SchemaVersion: 1, Tenant: tenantID, Project: project, Producer: task, ResultID: resultID, Result: root, Detection: detection}, nil
}
