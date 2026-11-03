package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	videoapi "photoeditor/backend/internal/video/api"
	"photoeditor/backend/internal/video/worker"
)

// installVideo enables the DEMO video API when VIDEO_STORE_DIR is set. Objects live in that directory;
// VIDEO_SCHEDULER_DATABASE_URL carries the photo_scheduler credential used to complete uploads inline.
func installVideo(ctx context.Context, api, portal *pgxpool.Pool, pepper []byte) (*videoapi.Service, func(), error) {
	root := os.Getenv("VIDEO_STORE_DIR")
	if root == "" {
		return nil, func() {}, nil
	}
	dsn := os.Getenv("VIDEO_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, func() {}, errors.New("VIDEO_SCHEDULER_DATABASE_URL is required with VIDEO_STORE_DIR")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, func() {}, errors.New("video scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(config)
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, func() {}, errors.New("video scheduler configuration invalid")
	}
	if err := database.RequireApplicationRole(ctx, scheduler, "photo_scheduler"); err != nil {
		scheduler.Close()
		return nil, func() {}, err
	}
	tmp := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		scheduler.Close()
		return nil, func() {}, err
	}
	return &videoapi.Service{Pool: api, Scheduler: scheduler, Portal: portal, Pepper: pepper, Store: worker.LocalStore{Root: filepath.Join(root, "objects")}, Bucket: "photo-video",
		Authz: tenantAuthorizer, TempDir: tmp}, scheduler.Close, nil
}

// tenantAuthorizer is the production role check: live membership through photo.authorize_tenant.
func tenantAuthorizer(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, required domain.Role) (domain.Role, error) {
	return tenant.AuthorizeInTransaction(ctx, tx, who, tid, required)
}
