package workflow

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2optical"
)

type PerspectiveLinesRecoveryConfig struct {
	RuntimeLockSHA string
	Timeout        time.Duration
}

func PerspectiveLinesRecoveryFromEnvironment(getenv func(string) string) (PerspectiveLinesRecoveryConfig, bool, error) {
	var cfg PerspectiveLinesRecoveryConfig
	if getenv == nil {
		return cfg, false, errors.New("perspective lines recovery environment unavailable")
	}
	flag := getenv("PHOTO_SCHEDULER_PERSPECTIVE_LINES_ENABLED")
	if flag == "" || flag == "false" {
		return cfg, false, nil
	}
	if flag != "true" {
		return cfg, false, errors.New("perspective lines recovery enabled flag invalid")
	}
	cfg.RuntimeLockSHA = getenv("PHOTO_SCHEDULER_PERSPECTIVE_LINES_RUNTIME_SHA")
	if domainDigestInvalid(cfg.RuntimeLockSHA) {
		return cfg, false, errors.New("perspective lines runtime digest invalid")
	}
	seconds := int64(300)
	if raw := getenv("PHOTO_SCHEDULER_PERSPECTIVE_LINES_TIMEOUT_SECONDS"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 || n > 300 {
			return cfg, false, errors.New("perspective lines timeout invalid")
		}
		seconds = n
	}
	cfg.Timeout = time.Duration(seconds) * time.Second
	return cfg, true, nil
}

// Source readers require the scheduler role. Shared pool/store ownership stays
// with RunScheduler; this processor starts no goroutines during construction.
func NewPerspectiveLinesRecoveryProcessor(ctx context.Context, pool *pgxpool.Pool, objects *objectstore.Store, bucket, workingICC string, cfg PerspectiveLinesRecoveryConfig) (*PerspectiveLinesRecoveryDispatcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pool == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 || cfg.Timeout <= 0 || cfg.Timeout > 5*time.Minute {
		return nil, errors.New("perspective lines recovery dependencies invalid")
	}
	policy, err := CompilePerspectiveLinesExecutionPolicy(workingICC, cfg.RuntimeLockSHA)
	if err != nil {
		return nil, err
	}
	if err := verifyPerspectiveLinesRecoveryInstallation(ctx, pool); err != nil {
		return nil, err
	}
	preparation := PerspectiveLinesPreparation{DB: pool, Documents: objects, Inputs: objects,
		Sources: SQLPerspectiveLinesSources{DB: pool, Bucket: bucket},
		Arrays:  v2optical.SQLPerspectiveSourceArrayCatalog{DB: pool, Bucket: bucket}, Bucket: bucket, Policy: policy}
	return &PerspectiveLinesRecoveryDispatcher{Candidates: SQLPerspectiveLinesRecoveryCandidates{DB: pool}, Resumer: preparation, Timeout: cfg.Timeout}, nil
}

func verifyPerspectiveLinesRecoveryInstallation(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pool == nil {
		return errors.New("perspective lines recovery database unavailable")
	}
	required := []string{
		"photo.next_v29_perspective_lines_recovery(uuid)",
		"photo.finalize_next_v29_perspective_lines()",
		"photo.expire_next_v29_perspective_lines_draft()",
		"photo.finalize_failed_v29_perspective_lines()",
		"photo.release_next_v29_perspective_lines_quota()",
		"photo.v29_perspective_lines_scheduler_recovery(uuid,uuid)",
		"photo.activate_v29_perspective_lines(uuid,uuid,jsonb)",
		"photo.v29_perspective_source_document(uuid,uuid,uuid,uuid,uuid,jsonb)",
		"photo.v29_perspective_array_source(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid,text,text)",
	}
	var installed bool
	err := pool.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='photo.runs'::regclass AND tgname='runs_v29_lines_admission_receipt_guard' AND tgenabled='O') AND coalesce(bool_and(
 coalesce(p.prosecdef AND owner.rolname='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE'),false)),false)
 FROM unnest($1::text[]) AS required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`, required).Scan(&installed)
	if err != nil || !installed {
		return errors.New("perspective lines recovery SQL installation unavailable")
	}
	return ctx.Err()
}
