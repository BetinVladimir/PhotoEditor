package workflow

import (
 "photoeditor/backend/internal/database"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
)

type RestorationAdoptionRuntimeConfig struct {
	CatalogDatabaseURL string
}

func RestorationAdoptionFromEnvironment(getenv func(string) string) (RestorationAdoptionRuntimeConfig, bool, error) {
	var config RestorationAdoptionRuntimeConfig
	if getenv == nil {
		return config, false, errors.New("restoration adoption environment unavailable")
	}
	switch getenv("PHOTO_V28_RESTORATION_ADOPTION_ENABLED") {
	case "", "false":
		return config, false, nil
	case "true":
		config.CatalogDatabaseURL = getenv("PHOTO_V28_RESTORATION_CATALOG_DATABASE_URL")
		if config.CatalogDatabaseURL == "" {
			return config, false, errors.New("restoration editor catalog database unavailable")
		}
		return config, true, nil
	default:
		return config, false, errors.New("invalid restoration adoption enabled flag")
	}
}

func NewRestorationAdoptionProcessor(ctx context.Context, schedulerDB *pgxpool.Pool,
	objects *objectstore.Store, bucket string, config RestorationAdoptionRuntimeConfig,
) (*v2draft.RestorationAdoptionDispatcher, func(), error) {
	noop := func() {}
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if schedulerDB == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 ||
		config.CatalogDatabaseURL == "" {
		return nil, noop, errors.New("restoration adoption dependencies invalid")
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	required := []string{
		"photo.next_v28_restoration_adoption(uuid)",
		"photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text)",
		"photo.v28_restoration_result_document(uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
		"photo.v28_restoration_result_array(uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
		"photo.adopt_v28_restoration_verified(uuid,uuid,uuid,text)",
		"photo.create_v28_restoration_review(uuid,uuid,uuid)",
		"photo.v28_glare_completed_source_ready(uuid,uuid,uuid)",
		"photo.withdraw_completed_glare_consent()",
		"photo.release_next_glare_withdrawn_quota()",
	}
	var installed bool
	err := schedulerDB.QueryRow(bounded, `SELECT current_user='photo_scheduler'
 AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND has_function_privilege(current_user,p.oid,'EXECUTE'),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`, required).Scan(&installed)
	if err != nil || !installed {
		return nil, noop, errors.New("restoration adoption SQL installation unavailable")
	}
	poolConfig, err := pgxpool.ParseConfig(config.CatalogDatabaseURL)
	if err != nil {
		return nil, noop, errors.New("restoration editor catalog database configuration invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	database.GuardApplicationRole(poolConfig)
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	catalogDB, err := pgxpool.NewWithConfig(bounded, poolConfig)
	if err != nil {
		return nil, noop, errors.New("restoration editor catalog database unavailable")
	}
	var editorReady bool
	err = catalogDB.QueryRow(bounded, `SELECT current_user='photo_api'
	 AND to_regprocedure('photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text)') IS NOT NULL`).Scan(&editorReady)
	if err != nil || !editorReady {
		catalogDB.Close()
		return nil, noop, errors.New("restoration editor catalog authority unavailable")
	}
	adopter := v2draft.SQLRestorationCandidateAdopter{CatalogDB: catalogDB,
		SchedulerDB: schedulerDB, Objects: objects, Bucket: bucket}
	processor := &v2draft.RestorationAdoptionDispatcher{
		Candidates: v2draft.SQLRestorationAdoptionQueue{DB: schedulerDB},
		Adopter:    adopter, Timeout: 10 * time.Minute}
	processor.Withdrawal = v2draft.SQLGlareAdoptionWithdrawal{DB: schedulerDB}
	return processor, catalogDB.Close, nil
}
