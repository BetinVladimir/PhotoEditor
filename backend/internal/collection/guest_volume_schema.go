package collection

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// RequireGuestVolumePublication checks the installed private gate and all
// three mounted consumers; an uninstalled migration must fail API startup.
func RequireGuestVolumePublication(ctx context.Context,db artifactjson.RowQuerier)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return errors.New("guest volume database required")}
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();var ready bool
 err:=db.QueryRow(ctx,`SELECT current_user='photo_api'
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.guest_volume_collection_current(uuid,uuid)')
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'c.state=''published'' AND c.deleted_at IS NULL FOR SHARE')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.privilege_type='EXECUTE' AND a.grantee<>p.proowner))
 AND (SELECT count(*)=3 FROM pg_proc p WHERE p.oid IN(
  to_regprocedure('photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz)'),
  to_regprocedure('photo.redeem_download_intent(bytea,text)'),
  to_regprocedure('photo.v30_delivery_download_intent_current(bytea,text,text,text,text,bigint,text)'))
  AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
  AND strpos(p.prosrc,'photo.guest_volume_collection_current(')>0
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.privilege_type='EXECUTE' AND a.grantee=0))`).Scan(&ready)
 if err!=nil{return err};if err=ctx.Err();err!=nil{return err};if !ready{return errors.New("guest volume publication schema unavailable")};return nil
}
