package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// This is a live discovery observation, not a materialization lease.
type RenderDerivativeIntent struct {
	Tenant, Run, Project, Task, Result uuid.UUID
	Root                               v2input.Ref
	SourceAttempt                      int
	SourceFence                        int64
	SRGBSHA                            string
}
type SQLRenderDerivativeIntents struct{ DB *pgxpool.Pool }

func (s SQLRenderDerivativeIntents) Next(ctx context.Context, after uuid.UUID) (*RenderDerivativeIntent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, renderUnavailable()
	}
	var cursor any
	if after != uuid.Nil {
		cursor = after
	}
	var value RenderDerivativeIntent
	err := s.DB.QueryRow(ctx, "SELECT * FROM photo.next_v26_render_derivative($1)", cursor).Scan(&value.Tenant, &value.Run, &value.Project, &value.Task, &value.Result, &value.Root.ArtifactID, &value.Root.SHA256, &value.SourceAttempt, &value.SourceFence, &value.SRGBSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value.Tenant == uuid.Nil || value.Run == uuid.Nil || value.Project == uuid.Nil || value.Task == uuid.Nil || value.Result == uuid.Nil || value.Root.ArtifactID == uuid.Nil || value.SourceAttempt < 1 || value.SourceFence < 1 {
		return nil, v2input.ErrInvalid
	}
	for _, digest := range []string{value.Root.SHA256, value.SRGBSHA} {
		if len(digest) != 64 {
			return nil, v2input.ErrInvalid
		}
		for _, c := range digest {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return nil, v2input.ErrInvalid
			}
		}
	}
	return &value, nil
}

// Reserve does not dispatch work; callers must still validate ICC/closure and publish task parameters.
func (s SQLRenderDerivativeIntents) Reserve(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tenant == uuid.Nil || run == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	if s.DB == nil {
		return false, renderUnavailable()
	}
	var accepted bool
	err := s.DB.QueryRow(ctx, "SELECT photo.reserve_v26_render_derivative($1,$2)", tenant, run).Scan(&accepted)
	return accepted, err
}

// Release frees only residual quota after terminal tasks and object writes have settled.
func (s SQLRenderDerivativeIntents) Release(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if tenant == uuid.Nil || run == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	if s.DB == nil {
		return false, renderUnavailable()
	}
	var accepted bool
	err := s.DB.QueryRow(ctx, "SELECT photo.release_v26_render_derivative_quota($1,$2)", tenant, run).Scan(&accepted)
	return accepted, err
}
