package interchange

import (
 "context"
 "errors"
 "time"

 "photoeditor/backend/internal/artifactjson"
)

// RequireImportReviewFinalAuthority checks the installed entrypoints once before
// advertising the API. It does not qualify image-processing runtimes.
func RequireImportReviewFinalAuthority(ctx context.Context, db artifactjson.RowQuerier) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return errors.New("interchange review database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(ctx,`SELECT current_user='photo_api' AND
 (SELECT count(*)=2 FROM pg_proc p WHERE p.oid IN (
 to_regprocedure('photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)'),
 to_regprocedure('photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb)'))
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND EXISTS(SELECT FROM unnest(p.proconfig) c WHERE replace(c,' ','')='search_path=pg_catalog,photo')
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,(SELECT oid FROM pg_roles WHERE rolname='photo_api')))
 AND strpos(p.prosrc,'-- 02569:')>0
 AND strpos(p.prosrc,'photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) IS DISTINCT FROM true')>0)
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.grant_v30_import_review(uuid,uuid,uuid,uuid,uuid,text,text,jsonb)')
 AND strpos(p.prosrc,'AND workflow_type=''interchange'' AND renderer_version=''v2.2'' FOR UPDATE;')>0
 AND strpos(p.prosrc,'a.expires_at>clock_timestamp()')>0
 AND strpos(p.prosrc,'-- 02569:')<strpos(p.prosrc,' IF FOUND THEN'||chr(10)||'  IF old.project_id<>w.project_id'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.revoke_v30_import_review(uuid,uuid,uuid,uuid,uuid,jsonb)')
 AND strpos(p.prosrc,'-- 02569:')<strpos(p.prosrc,' IF NOT FOUND OR old.proposal_sha<>proposal_sha'))`).Scan(&ready)
 if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if !ready{return errors.New("interchange review final authority schema unavailable")}
 return nil
}
