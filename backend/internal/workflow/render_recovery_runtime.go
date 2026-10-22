package workflow

import (
 "photoeditor/backend/internal/database"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
	"time"
)

// NewRenderRecoveryProcessor owns only its API-role pool. Scheduler pool and
// object client remain owned by RunScheduler; callers stop the loop before close.
func NewRenderRecoveryProcessor(ctx context.Context, scheduler *pgxpool.Pool, sourceDSN string, objects *objectstore.Store, bucket string) (*v2render.SQLRenderBatchRecovery, func(), error) {
	if scheduler == nil || scheduler.Config().MaxConns < 2 || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, nil, errors.New("render recovery dependencies invalid")
	}
	var schedulerRole string
	if err := scheduler.QueryRow(ctx, "SELECT current_user").Scan(&schedulerRole); err != nil || schedulerRole != "photo_scheduler" {
		return nil, nil, errors.New("render scheduler database role unavailable")
	}
	cfg, err := pgxpool.ParseConfig(sourceDSN)
	if err != nil {
		return nil, nil, errors.New("render source database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	database.GuardApplicationRole(cfg)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	cfg.MaxConns = 4
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, errors.New("render source database unavailable")
	}
	var role string
	if err = api.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "photo_api" {
		api.Close()
		return nil, nil, errors.New("render source database role unavailable")
	}
	return &v2render.SQLRenderBatchRecovery{Factory: v2render.SQLRenderBatchFactory{API: api, Scheduler: scheduler, Sources: objects, Documents: objects, SourceBucket: bucket, DocumentBucket: bucket}, Timeout: 2 * time.Minute}, api.Close, nil
}
