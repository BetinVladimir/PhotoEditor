package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// LensPreviewSource is a live read observation, not an admission lease.
// Every publication/seal/adoption must repeat these identities under SQL locks.
type LensPreviewSource struct {
 Ordinal int
	Tenant, Project, Run, Workflow, Producer, ResultID uuid.UUID
	Result                                             v2input.Ref
	Attempt                                            int
	Fence                                              int64
	Principal                                          auth.Identity
	Base                                               v2input.SessionVersionBinding
}

type SQLLensPreviewSource struct{ DB *pgxpool.Pool }

func (s SQLLensPreviewSource) Resolve(ctx context.Context, tenant, producer uuid.UUID) (LensPreviewSource, error) {
	var zero LensPreviewSource
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || producer == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	value := LensPreviewSource{Tenant: tenant, Producer: producer}
	err = tx.QueryRow(ctx, "SELECT * FROM photo.v25_lens_preview_source($1,$2)", tenant, producer).Scan(
		&value.Project, &value.Run, &value.Workflow, &value.ResultID, &value.Result.ArtifactID, &value.Result.SHA256,
		&value.Attempt, &value.Fence, &value.Principal.UserID, &value.Principal.SessionID,
		&value.Base.AssetVersionID, &value.Base.Source.ArtifactID, &value.Base.Source.SHA256, &value.Base.CanvasHash,&value.Ordinal)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	if err = validateLensPreviewSource(value); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return value, nil
}

// ValidateLensPreviewSource checks complete frozen Lens batch identity.
// It validates structure only; callers must retain current SQL/object authority.
func ValidateLensPreviewSource(value LensPreviewSource) error {
 return validateLensPreviewSource(value)
}

func validateLensPreviewSource(value LensPreviewSource) error {
	if value.Ordinal<0 || value.Ordinal>4999 || value.Tenant == uuid.Nil || value.Project == uuid.Nil || value.Run == uuid.Nil || value.Workflow == uuid.Nil || value.Producer == uuid.Nil || value.ResultID == uuid.Nil || value.Principal.UserID == uuid.Nil || value.Principal.SessionID == uuid.Nil || value.Attempt < 1 || value.Fence < 1 || value.Base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(value.Result) || !validLayerArtifactRef(value.Base.Source) || !validLayerArtifactRef(v2input.Ref{ArtifactID: value.Base.AssetVersionID, SHA256: value.Base.CanvasHash}) || value.Result.ArtifactID == value.Base.Source.ArtifactID {
		return v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(value.Run)
	if err != nil || workflow != value.Workflow {
		return v2input.ErrInvalid
	}
	producer, err := v2input.TaskID(workflow, "optical.lens", value.Ordinal)
	if err != nil || producer != value.Producer {
		return v2input.ErrInvalid
	}
	return nil
}
