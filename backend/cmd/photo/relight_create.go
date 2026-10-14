package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2portrait"
)

func buildRelightCreate(ctx context.Context, objects *objectstore.Store, bucket string) (httpapi.RelightSubmit, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V28_RELIGHT_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	dsn := os.Getenv("V28_RELIGHT_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("relight scheduler configuration unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("relight configuration unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, noop, errors.New("relight configuration invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, noop, errors.New("relight configuration read failed")
	}
	deployment, err := v2portrait.DecodeRelightDeployment(raw)
	if err != nil {
		return nil, noop, errors.New("relight configuration invalid")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("relight scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("relight scheduler unavailable")
	}
	var role string
	var installed bool
	err = pool.QueryRow(ctx, `SELECT current_user,to_regprocedure('photo.create_v28_relight_initial_draft(uuid,uuid,uuid,uuid,text,jsonb,text)') IS NOT NULL AND to_regprocedure('photo.read_v28_relight_request_receipt(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb)') IS NOT NULL AND to_regprocedure('photo.list_v28_relight_candidates(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid)') IS NOT NULL`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		pool.Close()
		return nil, noop, errors.New("relight installation unavailable")
	}
	service, err := deployment.Build(pool, objects, bucket)
	if err != nil {
		pool.Close()
		return nil, noop, errors.New("relight deployment refused")
	}
	return service, pool.Close, nil
}
