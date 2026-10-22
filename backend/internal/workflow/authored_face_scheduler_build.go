package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
)

// Absent configuration is explicitly unavailable rather than a fake model or
// an inferred qualification. Invalid configured inputs fail scheduler startup.
func buildAuthoredFaceParentScheduler(ctx context.Context, scheduler *pgxpool.Pool, objects *objectstore.Store, bucket, srgb string) (*AuthoredFaceParentProcessor, func(), error) {
	path, pin, dsn := os.Getenv("PHOTO_AUTHORED_FACE_DEPLOYMENT"), os.Getenv("PHOTO_AUTHORED_FACE_DEPLOYMENT_SHA256"), os.Getenv("PHOTO_AUTHORED_FACE_API_DATABASE_URL")
	if path == "" && pin == "" && dsn == "" {
		return nil, func() {}, nil
	}
	d, err := LoadAuthoredFaceDeployment(path, pin)
	if err != nil {
		return nil, nil, err
	}
	if dsn == "" {
		return nil, nil, ErrAuthoredFaceUnavailable
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, ErrAuthoredFaceUnavailable
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	database.GuardApplicationRole(cfg)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, ErrAuthoredFaceUnavailable
	}
	closeAPI := func() { api.Close() }
	if err = database.RequireApplicationRole(ctx, api, "photo_api"); err != nil {
		closeAPI()
		return nil, nil, err
	}
	for _, signature := range []string{"photo.authored_render_face_preparation_state(uuid,uuid,integer,text)", "photo.authored_render_face_decision_state(uuid,uuid,integer,text)", "photo.next_authored_render_face_preparation(uuid,uuid)", "photo.authored_render_skin_preparation_state(uuid,uuid,integer,text)", "photo.authored_render_face_preview_preparation_state(uuid,uuid,integer,text,text,text,text,text)"} {
		var ready bool
		err = scheduler.QueryRow(ctx, `SELECT coalesce((SELECT p.prosecdef AND p.proowner='photo_fn_owner'::regrole AND has_function_privilege(current_user,p.oid,'EXECUTE') FROM pg_proc p WHERE p.oid=to_regprocedure($1)),false)`, signature).Scan(&ready)
		if err != nil || !ready {
			closeAPI()
			return nil, nil, ErrAuthoredFaceUnavailable
		}
	}
	preparation, err := NewAuthoredFaceParentPreparation(scheduler, api, objects, bucket, srgb, d)
	if err != nil {
		closeAPI()
		return nil, nil, err
	}
	return &AuthoredFaceParentProcessor{DB: scheduler, Preparation: preparation, tenants: append([]uuid.UUID(nil), d.Tenants...), after: map[uuid.UUID]uuid.UUID{}}, closeAPI, nil
}
