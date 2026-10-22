package main

import (
 "photoeditor/backend/internal/database"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2optical"
)

func buildPerspective(ctx context.Context, objects *objectstore.Store, bucket string) (httpapi.PerspectiveSubmit, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V29_PERSPECTIVE_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	dsn := os.Getenv("V29_PERSPECTIVE_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("perspective scheduler configuration unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("perspective configuration unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, noop, errors.New("perspective configuration invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, noop, errors.New("perspective configuration read failed")
	}
	expectedSHA := os.Getenv("PHOTO_V29_PERSPECTIVE_CONFIG_SHA256")
	if _, err := domain.ParseDigest(expectedSHA); err != nil {
		return nil, noop, errors.New("perspective configuration digest unavailable")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return nil, noop, errors.New("perspective configuration digest mismatch")
	}
	deployment, err := v2optical.DecodePerspectiveDeployment(raw)
	if err != nil {
		return nil, noop, errors.New("perspective configuration invalid")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("perspective scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(config)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("perspective scheduler unavailable")
	}
	var role string
	var installed bool
	err = pool.QueryRow(ctx, `SELECT current_user,
 to_regprocedure('photo.seal_v29_perspective_graph(uuid,uuid,uuid,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.read_v29_perspective_queued_receipt(uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.v29_perspective_array_source(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.register_v29_perspective_transport(uuid,uuid,text,jsonb,jsonb,text)') IS NOT NULL
 AND to_regprocedure('photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		pool.Close()
		return nil, noop, errors.New("perspective installation unavailable")
	}
	service, err := deployment.Build(pool, objects, bucket)
	if err != nil {
		pool.Close()
		return nil, noop, errors.New("perspective deployment refused")
	}
	return service, pool.Close, nil
}
