package main

import (
 "photoeditor/backend/internal/database"
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2optical"
	"photoeditor/backend/internal/workflow"
	"strings"
	"time"
)

func buildPerspectiveLines(ctx context.Context, objects *objectstore.Store, bucket string) (httpapi.PerspectiveLinesSubmit, func(), error) {
	noop := func() {}
	cfg, on, err := workflow.PerspectiveLinesRecoveryFromEnvironment(func(k string) string {
		return os.Getenv(strings.Replace(k, "PHOTO_SCHEDULER_PERSPECTIVE_LINES_", "PHOTO_API_PERSPECTIVE_LINES_", 1))
	})
	if err != nil || !on {
		return nil, noop, err
	}
	dsn := os.Getenv("V29_PERSPECTIVE_LINES_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("perspective lines scheduler database configuration unavailable")
	}
	workingICC, err := colorprofile.LoadWorkSHA(os.Getenv("PHOTO_ICC_ROOT"))
	if err != nil {
		return nil, noop, err
	}
	policy, err := workflow.CompilePerspectiveLinesExecutionPolicy(workingICC, cfg.RuntimeLockSHA)
	if err != nil {
		return nil, noop, err
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("perspective lines database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(config)
	config.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	config.MaxConns = 4
	startup, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	pool, err := pgxpool.NewWithConfig(startup, config)
	if err != nil {
		return nil, noop, errors.New("perspective lines database unavailable")
	}
	if _, err = workflow.NewPerspectiveLinesRecoveryProcessor(startup, pool, objects, bucket, workingICC, cfg); err != nil {
		pool.Close()
		return nil, noop, err
	}
	var installed bool
	required := []string{
		"photo.v29_perspective_lines_submission_response(uuid,uuid,uuid,uuid,uuid,text,text,text)",
		"photo.bind_v29_perspective_lines_submission(uuid,uuid,uuid,uuid,uuid,text,text)",
		"photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text)",
		"photo.save_v29_perspective_line_detection_intent(uuid,uuid,uuid,text)",
		"photo.reserve_v29_perspective_line_detection(uuid,uuid)",
		"photo.seal_v29_perspective_lines_transport(uuid,uuid,jsonb,jsonb)",
	}
	err = pool.QueryRow(startup, `SELECT coalesce(bool_and(coalesce(has_function_privilege(current_user,to_regprocedure(signature),'EXECUTE'),false)),false) FROM unnest($1::text[]) required(signature)`, required).Scan(&installed)
	if err != nil || !installed {
		pool.Close()
		return nil, noop, errors.New("perspective lines submission installation unavailable")
	}
	service := workflow.PerspectiveLinesPreparation{DB: pool, Documents: objects, Inputs: objects, Sources: workflow.SQLPerspectiveLinesSources{DB: pool, Bucket: bucket}, Arrays: v2optical.SQLPerspectiveSourceArrayCatalog{DB: pool, Bucket: bucket}, Bucket: bucket, Policy: policy, SubmissionTimeout: cfg.Timeout}
	return service, pool.Close, nil
}
