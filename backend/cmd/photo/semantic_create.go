package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/semanticproposal"
)

// Owns only the newly opened scheduler pool. API/storage remain caller-owned.
func buildSemanticCreate(ctx context.Context, api *pgxpool.Pool, objects *objectstore.Store, bucket string) (httpapi.SemanticPropose, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V27_CREATE_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	dsn := os.Getenv("V27_CREATE_SCHEDULER_DATABASE_URL")
	icc := os.Getenv("PHOTO_ICC_ROOT")
	if dsn == "" || icc == "" {
		return nil, noop, errors.New("semantic create deployment inputs unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("semantic create configuration unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, noop, errors.New("semantic create configuration invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, noop, errors.New("semantic create configuration read failed")
	}
	color, err := semanticproposal.LoadTransferColorProfile(icc)
	if err != nil {
		return nil, noop, errors.New("semantic create ICC invalid")
	}
	deployment, err := semanticproposal.DecodeCreateDeployment(raw, color)
	if err != nil {
		return nil, noop, errors.New("semantic create configuration invalid")
	}
	scheduler, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, noop, errors.New("semantic create scheduler unavailable")
	}
	service, err := deployment.Build(ctx, api, scheduler, objects, bucket)
	if err != nil {
		scheduler.Close()
		return nil, noop, errors.New("semantic create installation refused")
	}
	return service, scheduler.Close, nil
}
