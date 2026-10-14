package workflow
import (
 "context"
 "time"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)
// EnsureDraft is called after historical admission probing. It preserves exact
// persisted preparation, and cannot reset a conflicting/active detector run.
func(r ReplacementDetectionDocuments) EnsureDraft(ctx context.Context)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=r.DB.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",scope.Tenant.String());err!=nil{return err}
 step,err:=r.Prepared.EnsurePerspectiveDetectionDraft(ctx,tx,r.Rebuilt,r.Retained,r.Mappings,r.Unchanged,r.Plan.Descriptor(),r.Snapshot);if err!=nil{return err}
 restored,err:=RestorePerspectiveReplacementDetectionPlan(ctx,step,r.Plan.Descriptor());if err!=nil{return err}
 if string(restored.Descriptor())!=string(r.Plan.Descriptor()){return artifactjson.ErrStale}
 if err=ctx.Err();err!=nil{return err};return tx.Commit(ctx)
}
