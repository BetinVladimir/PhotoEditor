package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Private helpers must remain inaccessible to service roles. Merely having the
// admission functions installed does not establish replacement execution support.
const perspectiveReplacementExecutionInstallationSQL = `
WITH helpers(signature,secure) AS (VALUES
 ('photo.v29_perspective_execution_session(jsonb)',false),
 ('photo.v29_perspective_execution_source_scope(uuid,uuid,uuid,uuid,uuid,uuid,jsonb)',true)
), routes(signature,source_required) AS (VALUES
 ('photo.v29_worker_perspective_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',true),
 ('photo.v29_worker_perspective_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)',false),
 ('photo.v29_perspective_adoption_evidence(uuid,uuid,integer,bigint,uuid,text)',true)
)
SELECT current_user='photo_scheduler'
AND (SELECT coalesce(bool_and(coalesce(owner.rolname='photo_fn_owner' AND p.prosecdef=h.secure
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND NOT EXISTS(SELECT FROM unnest(ARRAY['photo_scheduler','photo_worker','photo_api','photo_gateway','photo_portal','photo_auth','photo_publisher','photo_maintenance']) role_name WHERE has_function_privilege(role_name,p.oid,'EXECUTE')),false)),false)
 FROM helpers h LEFT JOIN pg_proc p ON p.oid=to_regprocedure(h.signature) LEFT JOIN pg_roles owner ON owner.oid=p.proowner)
AND (SELECT coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND strpos(pg_get_functiondef(p.oid),'v29_perspective_execution_session')>0
 AND (NOT route.source_required OR strpos(pg_get_functiondef(p.oid),'v29_perspective_execution_source_scope')>0),false)),false)
 FROM routes route LEFT JOIN pg_proc p ON p.oid=to_regprocedure(route.signature) LEFT JOIN pg_roles owner ON owner.oid=p.proowner)`

func verifyPerspectiveReplacementExecutionInstallation(ctx context.Context, pool *pgxpool.Pool) error {
 if err:=ctx.Err();err!=nil { return err }
 if pool==nil { return errors.New("perspective replacement execution database unavailable") }
 bounded,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 var installed bool
 if err:=pool.QueryRow(bounded,perspectiveReplacementExecutionInstallationSQL).Scan(&installed);err!=nil||!installed {
  return errors.New("perspective replacement execution SQL installation unavailable")
 }
 return ctx.Err()
}
