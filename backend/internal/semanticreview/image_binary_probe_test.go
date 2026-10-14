package semanticreview

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type imageBinaryMediaProbeQuery struct{ db ApprovedProtectionQueryer }
type imageBinaryMediaProbeRow struct{ pgx.Row }

func (r imageBinaryMediaProbeRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	*dest[7].(*string) = "text/plain"
	return nil
}
func (q imageBinaryMediaProbeQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return imageBinaryMediaProbeRow{q.db.QueryRow(ctx, sql, args...)}
}
