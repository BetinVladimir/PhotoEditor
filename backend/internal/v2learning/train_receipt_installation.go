package v2learning

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// Accepted receipts remain available even when the optional training scheduler
// is withdrawn. Validate this API-only boundary independently at startup.
func RequireAcceptedTrainingReceiptInstallation(ctx context.Context, db artifactjson.RowQuerier) error {
 if err := ctx.Err(); err != nil { return err }
 if db == nil { return errors.New("training receipt database required") }
 bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
 defer cancel()
 var ready bool
 err := db.QueryRow(bounded, `SELECT current_user='photo_api' AND EXISTS (
 SELECT FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.v23_training_accepted_receipt(uuid,uuid,uuid,uuid,uuid,text,text)')
 AND p.prosecdef AND p.provolatile='s' AND p.prorettype='jsonb'::regtype
 AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT EXISTS (SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.privilege_type='EXECUTE' AND a.grantee NOT IN(p.proowner,'photo_api'::regrole)))`).Scan(&ready)
 if err != nil { return err }
 if err := bounded.Err(); err != nil { return err }
 if !ready { return errors.New("training accepted receipt installation unavailable") }
 return nil
}
