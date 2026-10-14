package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type PerspectiveLinesLifecycle struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (p PerspectiveLinesLifecycle) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.DB == nil {
		return false, errors.New("perspective lines lifecycle database required")
	}
	work, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	progress := false
	for _, query := range []string{
		"SELECT photo.expire_next_v29_perspective_lines_draft()",
		"SELECT photo.finalize_next_v29_perspective_lines()",
		"SELECT photo.finalize_failed_v29_perspective_lines()",
		"SELECT photo.release_next_v29_perspective_lines_quota()",
	} {
		var changed bool
		if err := p.DB.QueryRow(work, query).Scan(&changed); err != nil {
			return progress, err
		}
		progress = progress || changed
	}
	return progress, nil
}
