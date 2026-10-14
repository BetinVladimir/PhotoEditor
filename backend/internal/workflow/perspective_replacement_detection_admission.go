package workflow
import (
 "bytes"
 "context"
 "time"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// WasAdmitted acknowledges the immutable original acceptance only. It performs
// no source IO or queue mutation and must never be used as result authority.
func(r ReplacementDetectionDocuments) WasAdmitted(ctx context.Context)(bool,error){
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return false,err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return false,err}
 workflow,err:=v2input.WorkflowID(scope.Run);if err!=nil{return false,err}
 expected,err:=canonical.Marshal(map[string]any{"run_id":scope.Run,"workflow_id":workflow,"task_id":task.ID,"state":"queued"});if err!=nil{return false,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=r.DB.Begin(ctx);if err!=nil{return false,err};if tx==nil{return false,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return false,err};if role!="photo_scheduler"{return false,v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",scope.Tenant.String());err!=nil{return false,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v29_replacement_detector_admission($1,$2,$3,$4)",scope.Tenant,scope.Run,string(r.Plan.Descriptor()),r.Snapshot).Scan(&raw);err!=nil{return false,err}
 admitted:=false
 if len(raw)>0{
  if len(raw)>2048{return false,v2input.ErrInvalid}
  normal,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normal,expected){return false,v2input.ErrInvalid}
  admitted=true
 }
 if err=ctx.Err();err!=nil{return false,err}
 if err=tx.Commit(ctx);err!=nil{return false,err};return admitted,nil
}
