package delivery

import (
 "context"
 "time"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
const watermarkSchedulerInstallationQuery=`SELECT current_user='photo_scheduler' AND coalesce(bool_and(coalesce(owner.rolname='photo_fn_owner'
 AND (NOT required.api OR p.prosecdef)
 AND has_function_privilege('photo_scheduler',p.oid,'EXECUTE')=required.api
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 AND NOT EXISTS(SELECT FROM pg_roles forbidden WHERE forbidden.rolname IN('photo_api','photo_worker','photo_gateway','photo_portal') AND has_function_privilege(forbidden.oid,p.oid,'EXECUTE')),false)),false)
 FROM (SELECT unnest($1::text[]) signature,true api UNION ALL SELECT unnest($2::text[]),false) required
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(required.signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`

// ValidateWatermarkSchedulerInstallation verifies exact ABI, owner and effective role
// isolation before continuation admission. It grants no tenant or source authority.
func ValidateWatermarkSchedulerInstallation(ctx context.Context,db ProofTransaction)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return v2input.ErrInvalid}
 api:=[]string{
 "photo.reserve_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,uuid,text)",
 "photo.complete_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid,text)",
 "photo.reserve_v30_watermark_presentation_params(uuid,uuid,integer,text,text,uuid,uuid,uuid,text)",
 "photo.complete_v30_watermark_presentation_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid,text)",
 "photo.reserve_v30_watermark_derivative_initial_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)",
 "photo.complete_v30_watermark_derivative_initial_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)",
 "photo.reserve_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)",
 "photo.complete_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)",
 "photo.reserve_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)",
 "photo.complete_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)",
 "photo.begin_v30_watermark_sharpen_continuation(uuid,uuid,integer,text,jsonb,text,text)",
 "photo.begin_v30_watermark_presentation_continuation(uuid,uuid,integer,text,jsonb,text,text)",
 "photo.begin_v30_watermark_encode_continuation(uuid,uuid,integer,text,jsonb,text,text)",
 "photo.read_v30_watermark_pin(uuid,uuid,uuid,uuid,uuid,text)",
 "photo.v30_watermark_stage_result(uuid,uuid,integer,text)",
 "photo.v30_watermark_pending_child_slots(uuid,uuid,integer)",
 "photo.v30_watermark_saved_continuation_params(uuid,uuid,integer,text,text)",
 "photo.v30_watermark_child_sources(uuid,uuid,integer,text,integer)",
 "photo.create_v30_watermark_initial_child_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text)",
 "photo.v30_watermark_initial_transport_ready(uuid,uuid,integer,jsonb,text,text)",
 "photo.activate_v30_watermark_initial_child(uuid,uuid,integer,text,uuid,text,text)",
 "photo.v30_watermark_initial_child_activated(uuid,uuid,integer,text,uuid,jsonb,text,text)",
 "photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text)",
 "photo.v30_watermark_derivative_initial_transport_ready(uuid,uuid,integer,jsonb,text)",
 "photo.activate_v30_watermark_derivative_initial_child(uuid,uuid,integer,text,uuid,text)",

 "photo.create_v30_watermark_encode_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint,text)",
 "photo.v30_watermark_encode_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text)",
 "photo.activate_v30_watermark_encode(uuid,uuid,integer,integer,uuid,jsonb,text)",
 "photo.v30_watermark_encode_activated(uuid,uuid,integer,integer,uuid,jsonb,text)",
 "photo.create_v30_watermark_presentation_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint,text)",
 "photo.v30_watermark_presentation_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text,text)",
 "photo.activate_v30_watermark_presentation(uuid,uuid,integer,integer,uuid,jsonb,text,text)",
 "photo.v30_watermark_presentation_activated(uuid,uuid,integer,integer,uuid,jsonb,text,text)",
 "photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint,text)",
 "photo.v30_watermark_sharpen_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text)",
 "photo.activate_v30_watermark_sharpen(uuid,uuid,integer,integer,uuid,jsonb,text)",
 "photo.v30_watermark_sharpen_activated(uuid,uuid,integer,integer,uuid,jsonb,text)",
 }
 private:=[]string{
 "photo.v30_watermark_placement_ready(bigint,bigint,bigint,bigint,jsonb)",
 "photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint)",
 "photo.v30_watermark_derivative_profile(text,text,text)",
 "photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint)",
 "photo.lock_v30_watermark_retained_source(uuid,uuid,uuid,uuid,uuid,text,text)",
 "photo.v30_watermark_retained_source_ready(uuid,uuid,uuid,uuid,uuid,text,text)",
 "photo.v30_watermark_encode_completion_ready(uuid,uuid,text)",
 "photo.v30_watermark_initial_stage_scope(uuid,uuid,integer,text,text)",
 "photo.v30_watermark_initial_params_scope(uuid,uuid,integer,text,text)",
 "photo.v30_watermark_derivative_initial_params_scope(uuid,uuid,integer,text)",
 "photo.commit_v30_watermark_initial_child(uuid,uuid,integer,text,uuid)",
 "photo.commit_v30_watermark_derivative_initial_child(uuid,uuid,integer,text,uuid)",

 "photo.v30_watermark_encode_params_scope(uuid,uuid,integer,text)",
 "photo.v30_watermark_presentation_params_scope(uuid,uuid,integer,text,text)",
 "photo.v30_watermark_after_presentation_scope(uuid,uuid,integer,text,text)",
 "photo.v30_watermark_sharpen_params_scope(uuid,uuid,integer,text)",
 }
 query,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel();var installed bool
 if err:=db.QueryRow(query,watermarkSchedulerInstallationQuery,api,private).Scan(&installed);err!=nil{return err}
 if !installed{return domain.NewError("DELIVERY_WATERMARK_UNAVAILABLE",503)}
 return ctx.Err()
}
