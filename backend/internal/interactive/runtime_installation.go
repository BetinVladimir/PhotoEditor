package interactive

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// The API must not fall back to heartbeat-only selection on an old database.
func RequireRuntimeInstallation(ctx context.Context,db artifactjson.RowQuerier)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return errors.New("interactive runtime installation database required")}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(bounded,`SELECT current_user='photo_api' AND EXISTS(
 SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.select_interactive_runtime(uuid,uuid,uuid,uuid,text)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND strpos(p.prosrc,'p_tenant=nullif(current_setting(''app.tenant_id'',true),'''')::uuid')>0
 AND strpos(p.prosrc,'p_operation IN(''mask.prompt'',''image.generate'')')>0
 AND strpos(p.prosrc,'photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)')>0
 AND strpos(p.prosrc,'w.execution_pool=''general''')>0
 AND strpos(p.prosrc,'n.enabled AND photo.capacity_policy_node_fresh(n)')>0
 AND strpos(p.prosrc,'photo.capacity_policy_worker_fresh(w)')>0
 AND strpos(p.prosrc,'photo.interactive_worker_discoverable(p_tenant,w.worker_id,p_operation,b.backend,w.lock_id)')>0
 AND strpos(p.prosrc,'photo.tenant_capabilities()')>0
 AND EXISTS(SELECT FROM pg_proc d WHERE d.oid=to_regprocedure('photo.interactive_worker_discoverable(uuid,uuid,text,text,text)')
 AND d.prosecdef AND pg_get_userbyid(d.proowner)='photo_fn_owner'
 AND replace(array_to_string(d.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND strpos(d.prosrc,'w.execution_pool=''general''')>0
 AND strpos(d.prosrc,'photo.capacity_policy_worker_fresh(w)')>0
 AND strpos(d.prosrc,'n.enabled AND photo.capacity_policy_node_fresh(n)')>0
 AND strpos(d.prosrc,'photo.editor_internal_dispatch_installed(p_operation)')>0
 AND strpos(d.prosrc,'photo.worker_authorized(')=0
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(d.proacl,acldefault('f',d.proowner))) da
 WHERE da.privilege_type='EXECUTE' AND da.grantee<>d.proowner))
 AND EXISTS(SELECT FROM pg_proc e WHERE e.oid=to_regprocedure('photo.editor_capability_ready(uuid,uuid,uuid,uuid,text)')
 AND e.prosecdef AND pg_get_userbyid(e.proowner)='photo_fn_owner'
 AND replace(array_to_string(e.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,e.oid,'EXECUTE')
 AND strpos(e.prosrc,'photo.interactive_worker_discoverable(p_tenant,w.worker_id,p_operation,''cpu'',NULL)')>0
 AND strpos(e.prosrc,'photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)')>0
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(e.proacl,acldefault('f',e.proowner))) ea
 WHERE ea.privilege_type='EXECUTE' AND ea.grantee NOT IN(e.proowner,'photo_api'::regrole)))
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,'photo_api'::regrole)))`).Scan(&ready)
 if err!=nil{return err};if err:=bounded.Err();err!=nil{return err};if !ready{return errors.New("interactive qualified runtime installation unavailable")};return nil
}
