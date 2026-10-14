package delivery

import (
 "context"
 "time"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
const watermarkAPIInstallationQuery=`SELECT current_user='photo_api' AND coalesce(bool_and(coalesce(owner.rolname='photo_fn_owner'
 AND (NOT required.api OR p.prosecdef)
 AND has_function_privilege('photo_api',p.oid,'EXECUTE')=required.api
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND NOT EXISTS(SELECT FROM pg_roles forbidden WHERE forbidden.rolname IN('photo_scheduler','photo_worker','photo_gateway','photo_portal') AND has_function_privilege(forbidden.oid,p.oid,'EXECUTE')),false)),false)
 FROM (SELECT unnest($1::text[]) signature,true api UNION ALL SELECT unnest($2::text[]),false) required
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(required.signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`

// ValidateWatermarkAPIInstallation verifies exact ABI, owner and effective role
// isolation before preparation. It grants no tenant or source authority.
func ValidateWatermarkAPIInstallation(ctx context.Context,db ProofTransaction)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return v2input.ErrInvalid}
 api:=[]string{
  "photo.v30_delivery_watermark_source(uuid,uuid,uuid,uuid,uuid,uuid)",
  "photo.pin_v30_watermark_source(uuid,uuid,uuid,uuid,text,text,text,text)",
  "photo.read_v30_watermark_pin_retry(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text)",
  "photo.select_v30_watermark_version(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text)",
 }
 private:=[]string{
  "photo.v30_watermark_pin_geometry(jsonb)",
  "photo.v30_watermark_pinned_source_ready(uuid,uuid,uuid,uuid,uuid,text,text)",
  "photo.guard_v30_watermark_sources()",
  "photo.guard_v30_watermark_selections()",
 }
 query,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel();var installed bool
 if err:=db.QueryRow(query,watermarkAPIInstallationQuery,api,private).Scan(&installed);err!=nil{return err}
 if !installed{return domain.NewError("DELIVERY_WATERMARK_UNAVAILABLE",503)}
 return ctx.Err()
}
