package workflow

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type ExportBatchFinalizer struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (f ExportBatchFinalizer) ProcessOne(ctx context.Context) (bool, error) {
	if f.DB == nil {
		return false, errors.New("export batch database required")
	}
	var ok bool
	e := f.DB.QueryRow(ctx, `SELECT photo.finalize_export_batch()`).Scan(&ok)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, e
}
