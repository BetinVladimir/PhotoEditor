package maintenance

import (
 "context"
 "encoding/hex"
 "errors"
 "strings"
 "time"
 "photoeditor/backend/internal/artifactjson"
)

// RequireEditorGCSchema refuses cleanup on a schema missing the editor roots or
// the conflicting artifact row-lock guards installed by migration02531.
func RequireEditorGCSchema(ctx context.Context, db artifactjson.RowQuerier) error {
 if db==nil { return errors.New("editor GC database required") }
 ctx,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel()
 guards:=[]struct{table string;fields []string}{
  {"editor_recipe_bridges",[]string{"override_artifact_id"}},
  {"editor_effect_builds",[]string{"source_artifact_id","frozen_recipe_artifact_id","executed_style_artifact_id","processed_source_artifact_id","quality_artifact_id","result_artifact_id","seal_params_artifact_id","seal_receipt_artifact_id","effect_source_artifact_id","effect_source_result_artifact_id","final_recipe_artifact_id"}},
 }
 for _,guard:=range guards {
  if err:=ctx.Err();err!=nil { return err }
  args:=hex.EncodeToString([]byte(strings.Join(guard.fields,"\x00")+"\x00"))
  var ready bool
  err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_trigger t
   WHERE t.tgrelid=to_regclass('photo.'||$1) AND t.tgname='ready_artifact_reference'
    AND NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23
    AND t.tgfoid=to_regprocedure('photo.guard_ready_artifact_reference()')
    AND t.tgnargs=$3 AND t.tgargs=decode($2,'hex'))
   AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.v2_artifact_has_references(uuid,uuid)')
    AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
    AND strpos(p.prosrc,'photo.'||$1)>0)`,guard.table,args,len(guard.fields)).Scan(&ready)
  if err!=nil { return err }
  if err:=ctx.Err();err!=nil { return err }
  if !ready { return errors.New("editor GC reference schema unavailable") }
 }
 return nil
}
