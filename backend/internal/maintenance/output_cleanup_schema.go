package maintenance

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// RequireOutputCleanupRefund inspects installed settlement owners, ACL and guard ordering.
func RequireOutputCleanupRefund(ctx context.Context, db artifactjson.RowQuerier) error {
 if db==nil{return errors.New("output cleanup database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 if err:=ctx.Err();err!=nil{return err}
 var ready bool
 err:=db.QueryRow(ctx,`SELECT
 (SELECT count(*)=2 FROM pg_attribute WHERE attrelid=to_regclass('photo.output_uploads')
  AND attname IN('returned_to_run_reservation','cleanup_ready_output') AND atttypid='boolean'::regtype AND attnotnull AND NOT attisdropped)
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.finish_output_cleanup_pending(uuid,uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'photo.lock_task(hint.task_id)')>0
  AND strpos(p.prosrc,'photo.lock_task(hint.task_id)')<strpos(p.prosrc,'FOR UPDATE;')
  AND strpos(p.prosrc,'q.principal_id=r.created_by')>0
  AND strpos(p.prosrc,'cleanup_lease_until<=clock_timestamp()')<strpos(p.prosrc,'SET bytes=bytes+u.bytes')
  AND strpos(p.prosrc,'returned_to_run_reservation=returned_to_run_reservation OR refund')>strpos(p.prosrc,'SET bytes=bytes+u.bytes')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.output_ready_orphan(uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'photo.task_results')>0 AND strpos(p.prosrc,'photo.artifact_has_references')>0
  AND strpos(p.prosrc,'a.gc_storage_settled')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.finish_output_cleanup(uuid,uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE')
  AND strpos(p.prosrc,'photo.output_ready_orphan(u.id)')>0
  AND strpos(p.prosrc,'photo.finish_artifact_gc(a.id,p_token)')>strpos(p.prosrc,'photo.output_ready_orphan(u.id)')
  AND strpos(p.prosrc,'NOT a.gc_storage_settled')>strpos(p.prosrc,'photo.finish_artifact_gc(a.id,p_token)')
  AND strpos(p.prosrc,'SET bytes=bytes+u.bytes')>strpos(p.prosrc,'NOT a.gc_storage_settled')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.renew_next_retry_cleanup_quota()')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'-- 02541:')>0 AND strpos(p.prosrc,'interval ''4 hours''')>0
  AND strpos(p.prosrc,'interval ''15 minutes''')>0
  AND strpos(p.prosrc,'SET expires_at=until_time')>strpos(p.prosrc,'q.expires_at<=clock_timestamp()')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.task_retry_outputs_pending(uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee<>p.proowner AND a.privilege_type='EXECUTE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.activate_task_retries(integer)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'AND NOT photo.task_retry_outputs_pending(id) ORDER BY')>0
  AND strpos(p.prosrc,'IF photo.task_retry_outputs_pending(t.id) THEN CONTINUE;END IF;')>strpos(p.prosrc,'t:=photo.lock_task(candidate)')
  AND strpos(p.prosrc,'IF photo.task_retry_outputs_pending(t.id) THEN CONTINUE;END IF;')<strpos(p.prosrc,'UPDATE photo.tasks SET state='))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.claim_output_cleanup()')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND has_function_privilege(current_user,p.oid,'EXECUTE')
  AND strpos(p.prosrc,'photo.lock_task(u.task_id)')>0
  AND strpos(p.prosrc,'photo.lock_task(u.task_id)')<strpos(p.prosrc,'SET state=''deleting''')
  AND strpos(p.prosrc,'photo.output_ready_orphan(u.id)')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))`).Scan(&ready)
 if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if !ready{return errors.New("output cleanup refund schema unavailable")}
 return nil
}
