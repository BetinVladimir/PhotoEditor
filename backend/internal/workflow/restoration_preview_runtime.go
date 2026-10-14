package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

func RestorationPreviewFromEnvironment(getenv func(string) string) (bool, error) {
	if getenv == nil {
		return false, errors.New("restoration preview environment unavailable")
	}
	switch getenv("PHOTO_V28_RESTORATION_PREVIEW_ENABLED") {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("invalid restoration preview enabled flag")
	}
}

func NewRestorationPreviewProcessor(ctx context.Context, pool *pgxpool.Pool,
	objects *objectstore.Store, bucket, workingSHA, srgbSHA string) (*RestorationPreviewProcessor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pool == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 ||
		domainDigestInvalid(workingSHA) || domainDigestInvalid(srgbSHA) {
		return nil, errors.New("restoration preview dependencies invalid")
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	required := []string{
		"photo.next_v28_restoration_preview_producer(uuid,text)",
		"photo.v28_restoration_preview_source(uuid,uuid)",
		"photo.save_v28_restoration_preview_intent(uuid,uuid,uuid,uuid,integer,bigint,uuid,text,text,uuid)",
		"photo.v28_restoration_preview_object(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text)",
		"photo.reserve_v28_restoration_preview(uuid,uuid)",
		"photo.release_v28_restoration_preview_quota(uuid,uuid)",
		"photo.reserve_v28_restoration_preview_document(uuid,uuid,uuid,text,text,text,bigint,text,text,text)",
		"photo.complete_v28_restoration_preview_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,text,text,text)",
		"photo.materialize_v28_restoration_preview_task(uuid,uuid,text,text,text,jsonb)",
		"photo.v28_restoration_preview_array(uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)",
		"photo.v28_restoration_result_array(uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
		"photo.seal_v28_restoration_preview(uuid,uuid,text,text,text,jsonb)",
		"photo.v28_restoration_preview_receipt(uuid,uuid,uuid,uuid,uuid,text,text)",
		"photo.v28_restoration_review_selection(uuid,uuid)",
		"photo.v28_restoration_review_completion_ready(uuid,uuid)",
		"photo.v28_restoration_selected_run_ready(uuid,uuid)",
		"photo.finalize_v28_restoration_selected_run()",
	}
	var installed bool
	err := pool.QueryRow(bounded, `SELECT current_user='photo_scheduler'
 AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`, required).Scan(&installed)
	if err != nil || !installed {
		return nil, errors.New("restoration preview SQL installation unavailable")
	}
	return &RestorationPreviewProcessor{
		Source: SQLRestorationPreviewProducers{DB: pool},
		Preparer: RestorationPreviewCoordinator{DB: pool, Objects: objects,
			Bucket: bucket, WorkingSHA: workingSHA, SRGBSHA: srgbSHA},
		SRGBSHA: srgbSHA,
	}, nil
}
