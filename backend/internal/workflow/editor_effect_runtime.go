package workflow

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2render"
)

func loadEditorRenderCalibration(getenv func(string)string,load func(string,string)(v2render.RenderCalibrationCatalog,error))(v2render.RenderCalibrationCatalog,error){
 if getenv==nil||load==nil{return nil,errors.New("editor render calibration loader unavailable")}
 file,digest:=getenv("PHOTO_RENDER_CALIBRATION_FILE"),getenv("PHOTO_RENDER_CALIBRATION_SHA256")
 if file==""||digest==""{return nil,errors.New("Editor requires PHOTO_RENDER_CALIBRATION_FILE and PHOTO_RENDER_CALIBRATION_SHA256")}
 catalog,err:=load(file,digest);if err!=nil||catalog==nil{return nil,errors.New("editor render calibration catalog unavailable or invalid")};return catalog,nil
}

// Startup validates installed authority, without invoking any lifecycle write.
func validateEditorEffectsRuntimeSQL(ctx context.Context,db *pgxpool.Pool)error{
 if err:=ctx.Err();err!=nil{return err};if db==nil{return errors.New("editor effects database unavailable")}
 var ready bool
 err:=db.QueryRow(ctx,`SELECT current_user='photo_scheduler' AND count(*)=7 AND coalesce(bool_and(
 p.prosecdef AND owner.rolname='photo_fn_owner'
 AND ('search_path=pg_catalog,photo'=ANY(p.proconfig) OR 'search_path=pg_catalog, photo'=ANY(p.proconfig))
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
  WHERE acl.privilege_type='EXECUTE' AND acl.grantee NOT IN(p.proowner,'photo_scheduler'::regrole::oid))),false)
 FROM (VALUES
 ('photo.editor_effect_seal_context(uuid)'),
 ('photo.editor_effect_final_context(uuid)'),
 ('photo.begin_editor_effect_final_render(uuid,uuid,uuid,uuid,text,text,bigint,jsonb)'),
 ('photo.activate_editor_effect_final_render(uuid,uuid,uuid)'),
 ('photo.settle_editor_effect_final_render()'),
 ('photo.fail_editor_effect_final_source(uuid,uuid,text,text,text,text,bigint,text,text)'),
 ('photo.withdraw_editor_effect_build()')) signatures(name)
 JOIN pg_proc p ON p.oid=to_regprocedure(signatures.name)
 JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&ready)
 if err!=nil||!ready{return errors.New("editor effects lifecycle SQL unavailable or incorrectly authorized")};return ctx.Err()
}
