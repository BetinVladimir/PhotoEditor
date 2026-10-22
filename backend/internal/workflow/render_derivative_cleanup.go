package workflow

import (
	"context"
	"errors"
	"time"
	"github.com/jackc/pgx/v5"
)

type RenderDerivativeCleanup struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (p RenderDerivativeCleanup) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.DB == nil {
		return false, errors.New("derivative cleanup database required")
	}
	// One budget covers the whole lifecycle pass, including lock waits. The
	// expansion loop deliberately delegates work deadlines to each processor.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
 changed := false
 for _, query := range []string{
  "SELECT photo.cancel_stale_v26_render_derivative()",
  "SELECT photo.finalize_successful_v26_render_derivative()",
  "SELECT photo.finalize_failed_v26_render_derivative()",
  "SELECT photo.release_next_v26_render_derivative_quota()",
  "SELECT photo.cancel_stale_semantic_preapply_preview()",
  "SELECT photo.finalize_next_semantic_preapply_preview()",
  "SELECT photo.release_next_semantic_preapply_preview_quota()",
 } {
  if err := ctx.Err(); err != nil { return changed, err }
  var receipt bool
  if err := p.DB.QueryRow(ctx, query).Scan(&receipt); err != nil { return changed, err }
  changed = changed || receipt
 }
 return changed, ctx.Err()
}
