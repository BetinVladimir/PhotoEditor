package capability

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// Missing the capability expiry filter must refuse API startup, rather than
// presenting a live signer heartbeat as renewed operator qualification.
func RequireExecutionPoolInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return errors.New("capability installation database required")}
 bounded,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 var ready bool
 err:=db.QueryRow(bounded,`SELECT current_user='photo_api' AND EXISTS(
 SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.tenant_capabilities()')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND strpos(p.prosrc,'WHEN op.operation=''provenance.sign'' THEN')>0
 AND strpos(p.prosrc,'w.execution_pool=''signer'' AND w.signer_qualified_until>clock_timestamp()')>0
 AND strpos(p.prosrc,'ELSE w.execution_pool=''general'' END')>0
 AND strpos(p.prosrc,'photo.capacity_policy_worker_fresh(w)')>0
 AND strpos(p.prosrc,'photo.capacity_policy_node_fresh(n)')>0
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))`).Scan(&ready)
 if err!=nil{return err};if err:=bounded.Err();err!=nil{return err}
 if !ready{return errors.New("capability execution pool installation unavailable")};return nil
}
