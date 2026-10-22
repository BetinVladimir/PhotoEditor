package portal

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// Preparation recovery is mandatory for the mounted Portal action. An older
// database must not start successfully and fail only after a lost response.
func RequirePreparationRecoveryInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
 if err := ctx.Err(); err != nil { return err }
 if db == nil { return errors.New("portal preparation database required") }
 bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
 defer cancel()
 var ready bool
 err := db.QueryRow(bounded, `SELECT current_user='photo_portal' AND EXISTS (
 SELECT FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.abandon_portal_download_preparation(bytea,bytea,text,text,text,uuid,uuid)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT EXISTS (SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,'photo_portal'::regrole)))`).Scan(&ready)
 if err != nil { return err }
 if err := bounded.Err(); err != nil { return err }
 if !ready { return errors.New("portal preparation recovery installation unavailable") }
 return nil
}
