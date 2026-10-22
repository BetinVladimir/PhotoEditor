package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// RenderQualification identifies the exact DBA-selected trusted report policy.
// It does not supply provider peak/resource calibration or activate rendering.
type RenderQualification struct {
	ModelSHA   string `json:"model_lock_sha"`
	RuntimeSHA string `json:"runtime_lock_sha"`
	Backend    string `json:"backend"`
	DeviceSHA  string `json:"device_profile_sha"`
	DatasetSHA string `json:"dataset_sha"`
}

type SQLRenderQualificationResolver struct{ API *pgxpool.Pool }

func (s SQLRenderQualificationResolver) Resolve(ctx context.Context, who auth.Identity, tenantID, projectID uuid.UUID) (RenderQualification, error) {
	if err := ctx.Err(); err != nil {
		return RenderQualification{}, err
	}
	if s.API == nil || tenantID == uuid.Nil || projectID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return RenderQualification{}, v2input.ErrInvalid
	}
	var out RenderQualification
	err := tenant.WithTenant(ctx, s.API, who, tenantID, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var currentRole string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&currentRole); err != nil {
			return err
		}
		if currentRole != "photo_api" {
			return v2input.ErrInvalid
		}
		if _, err := project.Authorize(ctx, tx, tenantID, projectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha FROM photo.resolve_v26_render_qualification($1,$2)`, tenantID, projectID).Scan(&out.ModelSHA, &out.RuntimeSHA, &out.Backend, &out.DeviceSHA, &out.DatasetSHA); err != nil { return err }
		// Qualification discovery can wait on concurrent catalog work. Withdrawn
		// authority must not receive the result of an earlier authorized read.
		currentAuthorityRole, err := tenant.AuthorizeInTransaction(ctx, tx, who, tenantID, domain.Editor)
		if err != nil { return err }
		_, err = project.Authorize(ctx, tx, tenantID, projectID, who.UserID, currentAuthorityRole, domain.Editor, false)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RenderQualification{}, domain.NewError("RENDER_UNAVAILABLE", 503)
	}
	if err != nil {
		return RenderQualification{}, err
	}
	for _, pin := range []string{out.ModelSHA, out.RuntimeSHA, out.DeviceSHA, out.DatasetSHA} {
		if _, err := domain.ParseDigest(pin); err != nil {
			return RenderQualification{}, v2input.ErrInvalid
		}
	}
	if out.Backend != "cpu" && out.Backend != "cuda" && out.Backend != "mps" {
		return RenderQualification{}, v2input.ErrInvalid
	}
	return out, nil
}
