package maintenance

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

func RequireResourceWithdrawal(ctx context.Context,db artifactjson.RowQuerier)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return errors.New("resource withdrawal database required")}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(bounded,`SELECT current_user='photo_maintenance'
 AND EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.deletion_jobs'::regclass AND attname='resource_cleanup_retry_at' AND NOT attisdropped AND atttypid='timestamptz'::regtype)
 AND (SELECT count(*)=3 FROM (VALUES
 ('photo.claim_resource_withdrawal()','record'::regtype::oid,true,true),
 ('photo.finish_resource_withdrawal(uuid,uuid,text,uuid,uuid)','boolean'::regtype::oid,true,false),
 ('photo.withdrawal_runs(uuid,text,uuid)','uuid'::regtype::oid,false,true)) expected(signature,result_type,exposed,returns_set)
 JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature) AND p.prorettype=expected.result_type
 WHERE pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname=CASE WHEN expected.exposed THEN 'plpgsql' ELSE 'sql' END)
 AND p.prosecdef=expected.exposed AND p.proretset=expected.returns_set
 AND (expected.signature<>'photo.claim_resource_withdrawal()' OR (
 p.proallargtypes=ARRAY['uuid'::regtype::oid,'uuid'::regtype::oid,'uuid'::regtype::oid,'text'::regtype::oid,'uuid'::regtype::oid]
 AND array_to_string(p.proargmodes,',')='t,t,t,t,t'
 AND array_to_string(p.proargnames,',')='job_id,cleanup_token,tenant_id,resource_type,resource_id'))
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND (NOT expected.exposed OR has_function_privilege(current_user,p.oid,'EXECUTE'))
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.privilege_type='EXECUTE'
 AND a.grantee<>p.proowner AND (NOT expected.exposed OR a.grantee<>(SELECT oid FROM pg_roles WHERE rolname='photo_maintenance'))))`).Scan(&ready)
 if err!=nil{return err};if err=bounded.Err();err!=nil{return err};if !ready{return errors.New("resource withdrawal installation unavailable")};return nil
}
