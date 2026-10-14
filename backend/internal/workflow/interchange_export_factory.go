package workflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

// SchedulerInterchangeExportFactory is supplied by the executable after it
// verifies a pinned native deployment and person-origin trust. The returned
// job owns its claim heartbeat and physically qualifies the full closure.
type SchedulerInterchangeExportFactory interface {
	BuildSchedulerInterchangeExport(context.Context, *pgxpool.Pool,
		*objectstore.Store, string) (ExpansionJob, error)
}

func installSchedulerInterchangeExport(ctx context.Context,
	factory SchedulerInterchangeExportFactory, db *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (ExpansionJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if factory == nil || db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, errors.New("interchange export installation invalid")
	}
	job, err := factory.BuildSchedulerInterchangeExport(ctx, db, objects, bucket)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, errors.New("interchange export processor unavailable")
	}
	return job, ctx.Err()
}
