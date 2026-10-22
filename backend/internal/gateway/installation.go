package gateway

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type outputInstallationReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RequireOutputFences inspects actual installed bodies and effective privileges
// before HTTP advertisement. It is not execution evidence for lock races.
func RequireOutputFences(ctx context.Context, db outputInstallationReader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return errors.New("gateway output installation unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var begin, complete, result string
	var permitted bool
	err := db.QueryRow(bounded, `WITH bodies AS (
 SELECT p.oid,p.prosrc,p.prosecdef,pg_get_userbyid(p.proowner) AS owner
 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo'),
 b AS (SELECT * FROM bodies WHERE oid=to_regprocedure('photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text)')),
 c AS (SELECT * FROM bodies WHERE oid=to_regprocedure('photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text)')),
 r AS (SELECT * FROM bodies WHERE strpos(prosrc,' INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)')>0)
 SELECT coalesce((SELECT prosrc FROM b),''),coalesce((SELECT prosrc FROM c),''),
 coalesce((SELECT min(prosrc) FROM r),''),
 (SELECT count(*)=1 AND bool_and(prosecdef AND owner='photo_fn_owner') FROM r)
 AND EXISTS(SELECT 1 FROM b WHERE prosecdef AND owner='photo_fn_owner' AND has_function_privilege(current_user,oid,'EXECUTE'))
 AND EXISTS(SELECT 1 FROM c WHERE prosecdef AND owner='photo_fn_owner' AND has_function_privilege(current_user,oid,'EXECUTE'))
 AND EXISTS(SELECT 1 FROM bodies WHERE oid=to_regprocedure('photo.authorize_gateway_task(uuid,integer,bigint,uuid,text)') AND prosecdef AND owner='photo_fn_owner' AND strpos(prosrc,'photo.execution_source_live(p_task)')>0)
 AND EXISTS(SELECT 1 FROM pg_proc helper WHERE helper.oid=to_regprocedure('photo.execution_source_live(uuid)') AND helper.prosecdef AND pg_get_userbyid(helper.proowner)='photo_fn_owner' AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(helper.proacl,acldefault('f',helper.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') AND NOT has_function_privilege(current_user,helper.oid,'EXECUTE'))
 AND EXISTS(SELECT 1 FROM pg_proc image_begin WHERE image_begin.oid=to_regprocedure('photo.begin_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text)') AND image_begin.prosecdef AND pg_get_userbyid(image_begin.proowner)='photo_fn_owner' AND image_begin.proconfig=ARRAY['search_path=pg_catalog, photo']::text[] AND has_function_privilege(current_user,image_begin.oid,'EXECUTE') AND strpos(image_begin.prosrc,'INTRINSIC_IMAGE_MEDIA_INVALID')>0 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(image_begin.proacl,acldefault('f',image_begin.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT 1 FROM pg_proc image_complete WHERE image_complete.oid=to_regprocedure('photo.complete_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,integer,integer)') AND image_complete.prosecdef AND pg_get_userbyid(image_complete.proowner)='photo_fn_owner' AND image_complete.proconfig=ARRAY['search_path=pg_catalog, photo']::text[] AND has_function_privilege(current_user,image_complete.oid,'EXECUTE') AND strpos(image_complete.prosrc,'INTRINSIC_IMAGE_METADATA_CONFLICT')>0 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(image_complete.proacl,acldefault('f',image_complete.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
 AND to_regprocedure('photo.gateway_input(uuid,integer,bigint,uuid,text,uuid)') IS NOT NULL`).Scan(&begin, &complete, &result, &permitted)
	if err != nil {
		return err
	}
	if !permitted || !installedOutputFences(begin, complete, result) {
		return errors.New("gateway output/result fence migrations or privileges missing")
	}
	return bounded.Err()
}
func installedOutputFences(begin, complete, result string) bool {
	ordered := func(source string, parts ...string) bool {
		at := 0
		for _, part := range parts {
			next := strings.Index(source[at:], part)
			if next < 0 {
				return false
			}
			at += next + len(part)
		}
		return true
	}
	return ordered(begin, "FOR UPDATE;", "-- 02536:", "photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)", "old.state='ready'") &&
		ordered(begin, "FOR UPDATE OF x;", "-- 02536:", "photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)", "q.expires_at<=clock_timestamp()", "UPDATE photo.quota_reservations SET bytes=bytes-p_bytes") &&
		ordered(complete, "FOR UPDATE;", "-- 02536:", "photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)", "IF u.id IS NULL", "IF u.state='ready'", "INSERT INTO photo.artifacts") &&
		ordered(result, "OUTPUT_NOT_VERIFIED", "-- 02535:", "t.lease_until<=clock_timestamp()", "slot.lease_until<=clock_timestamp()", "-- 02537:", "photo.execution_source_live(p_task)", " INSERT INTO photo.task_results(")
}
