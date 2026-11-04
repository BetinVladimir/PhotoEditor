package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

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
// VIDEO_SCHEDULER_DATABASE_URL carries the photo_scheduler credential used to complete uploads inline;
// the optional VIDEO_MAINTENANCE_DATABASE_URL (photo_maintenance) starts the housekeeping sweeper.
func installVideo(ctx context.Context, api, portal *pgxpool.Pool, pepper []byte) (*videoapi.Service, func(), error) {
	root := os.Getenv("VIDEO_STORE_DIR")
	if root == "" {
		return nil, func() {}, nil
	}
	dsn := os.Getenv("VIDEO_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, func() {}, errors.New("VIDEO_SCHEDULER_DATABASE_URL is required with VIDEO_STORE_DIR")
	}
	scheduler, err := videoRolePool(ctx, dsn, "photo_scheduler")
	if err != nil {
		return nil, func() {}, err
	}
	tmp := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		scheduler.Close()
		return nil, func() {}, err
	}
	var store worker.Store = worker.LocalStore{Root: filepath.Join(root, "objects")}
	var multipart videoapi.Multipart
	// VIDEO_S3=endpoint,access,secret[,public-endpoint] moves objects to MinIO/S3 and enables presigned multipart uploads;
	// the video-worker process must use the same store.
	if spec := os.Getenv("VIDEO_S3"); spec != "" {
		f := strings.Split(spec, ",")
		if len(f) < 3 {
			scheduler.Close()
			return nil, func() {}, errors.New("VIDEO_S3 wants endpoint,access,secret[,public-endpoint]")
		}
		public := ""
		if len(f) > 3 {
			public = f[3]
		}
		s3, err := worker.NewS3Store(f[0], public, f[1], f[2], os.Getenv("VIDEO_S3_TLS") == "1")
		if err != nil {
			scheduler.Close()
			return nil, func() {}, errors.New("VIDEO_S3 configuration invalid")
		}
		if err := s3.EnsureBucket(ctx, "photo-video"); err != nil {
			scheduler.Close()
			return nil, func() {}, errors.New("video object store unavailable")
		}
		store, multipart = s3, s3
	}
	svc := &videoapi.Service{Pool: api, Scheduler: scheduler, Portal: portal, Pepper: pepper, Store: store, Multipart: multipart, Bucket: "photo-video", Authz: tenantAuthorizer, TempDir: tmp}
	closeAll := scheduler.Close
	// Housekeeping (stuck uploads, abandoned ingests, withdrawn media, expired traffic leases, old autosaves) needs the
	// photo_maintenance credential; without it the API runs but nothing sweeps.
	if mdsn := os.Getenv("VIDEO_MAINTENANCE_DATABASE_URL"); mdsn != "" {
		maintenance, err := videoRolePool(ctx, mdsn, "photo_maintenance")
		if err != nil {
			scheduler.Close()
			return nil, func() {}, err
		}
		sweepCtx, stop := context.WithCancel(ctx)
		go (&worker.Sweeper{Pool: maintenance, Store: store}).Loop(sweepCtx, 30*time.Second)
		closeAll = func() { stop(); maintenance.Close(); scheduler.Close() }
	}
	return svc, closeAll, nil
}

// videoRolePool opens a pool that may only act as the given database role.
func videoRolePool(ctx context.Context, dsn, role string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("video " + role + " configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = role
	database.GuardApplicationRole(config)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("video " + role + " configuration invalid")
	}
	if err := database.RequireApplicationRole(ctx, pool, role); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// tenantAuthorizer is the production role check: live membership through photo.authorize_tenant.
func tenantAuthorizer(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, required domain.Role) (domain.Role, error) {
	return tenant.AuthorizeInTransaction(ctx, tx, who, tid, required)
}
