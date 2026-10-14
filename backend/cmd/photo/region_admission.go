package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2region"
)

func buildRegionAdmission(ctx context.Context, api *pgxpool.Pool, dsn string, objects *objectstore.Store, bucket string, catalog *v2region.PinnedRegionCalibrationCatalog, evidenceSHA string) (httpapi.RegionQueue, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if catalog == nil {
		return nil, noop, nil
	}
	if api == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, noop, errors.New("region dependencies unavailable")
	}
	required := []string{"resolve_v26_region_qualification", "v26_region_qualification_pin_ready", "v26_region_admission_scope_scheduler", "v26_region_seal_scope_scheduler", "freeze_v26_region_snapshot", "seal_v2_graph"}
	var installed bool
	if err := api.QueryRow(ctx, `SELECT count(DISTINCT p.proname)=cardinality($1::text[]) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname=ANY($1::text[])`, required).Scan(&installed); err != nil {
		return nil, noop, err
	}
	if !installed {
		return nil, noop, errors.New("region migrations unavailable")
	}
	profiles, err := v2region.NewSQLRegionProfileProvider(api, catalog, evidenceSHA)
	if err != nil {
		return nil, noop, errors.New("region calibration evidence unavailable")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("region database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.MaxConns = 8
	config.MinConns = 0
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("region database unavailable")
	}
	for _, check := range []struct {
		pool *pgxpool.Pool
		role string
	}{{api, "photo_api"}, {scheduler, "photo_scheduler"}} {
		var role string
		if err := check.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != check.role {
			scheduler.Close()
			return nil, noop, errors.New("region database role unavailable")
		}
	}
	return v2region.QueueService{Preparation: v2region.SQLRegionPreparation{API: api, Objects: objects, Bucket: bucket},
		DB: api, Scheduler: scheduler, Profiles: profiles, Dependencies: v2region.SQLRegionDependencies{API: api, Scheduler: scheduler, Objects: objects, Bucket: bucket}}, scheduler.Close, nil
}
