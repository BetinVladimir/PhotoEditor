package v2region

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5"
)
type recoveryInstallationQuery interface{QueryRow(context.Context,string,...any)pgx.Row}
// Mandatory API startup fence even when fresh Region qualification is disabled.
func RequireRecoveryInstallation(ctx context.Context,db recoveryInstallationQuery)error{
 if ctx.Err()!=nil{return ctx.Err()};if db==nil{return errors.New("region recovery database unavailable")}
 bounded,cancel:=context.WithTimeout(ctx,3*time.Second);defer cancel();var installed bool
 err:=db.QueryRow(bounded,`SELECT current_user='photo_api' AND EXISTS(SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_roles owner ON owner.oid=p.proowner WHERE n.nspname='photo' AND p.proname='v26_region_accepted_recovery' AND pg_get_function_identity_arguments(p.oid)='p_tenant uuid, p_asset uuid, p_actor uuid, p_session uuid, p_key text, p_sha text, p_run uuid, p_workflow uuid, p_task uuid' AND owner.rolname='photo_fn_owner' AND p.prosecdef AND p.proconfig @> ARRAY['search_path=pg_catalog, photo']::text[] AND has_function_privilege('photo_api',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE') AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE') AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))`).Scan(&installed)
 if err!=nil{return err};if bounded.Err()!=nil{return bounded.Err()};if !installed{return errors.New("region recovery installation unavailable")};return nil
}
