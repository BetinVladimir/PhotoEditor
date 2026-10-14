package workflow
import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type ReplacementDetectionAdoptionLoader struct{
 DB v2optical.NegativeTransactions
 Reader v2input.SessionAnalysisObjectReader
 Bucket string
}
// Load discards discovery authority and reconstructs the complete current
// original-version and mapped physical source package. Final commit repeats it.
func(l ReplacementDetectionAdoptionLoader) Load(ctx context.Context,c ReplacementDetectionAdoptionCandidate)(ReplacementDetectionDocuments,error){
 var zero ReplacementDetectionDocuments
 if err:=ctx.Err();err!=nil{return zero,err}
 d,err:=decodeReplacementDetectionDescriptor(c.Descriptor);if err!=nil{return zero,err}
 if l.DB==nil||l.Reader==nil||len(l.Bucket)<3||len(l.Bucket)>63||d.Tenant!=c.Tenant||d.Run!=c.Run||d.Task!=c.Task||c.Snapshot==uuid.Nil{return zero,v2input.ErrInvalid}
 who:=auth.Identity{UserID:d.Actor,SessionID:d.Session}
 decision:=v2optical.NegativeDeclinedDecision{Review:d.Review,DecisionHash:d.DecisionHash,Route:"base"}
 err=func()error{
  bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
  tx,err:=l.DB.Begin(bounded);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
  defer tx.Rollback(context.WithoutCancel(bounded))
  var role string
  if err=tx.QueryRow(bounded,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
  if _,err=tx.Exec(bounded,"SELECT set_config('app.tenant_id',$1,true)",c.Tenant.String());err!=nil{return err}
  if err=tx.QueryRow(bounded,"SELECT origin_task_id,origin_run_id FROM photo.read_v29_replacement_detector_decision($1,$2,$3,$4)",c.Tenant,c.Run,string(c.Descriptor),c.Snapshot).Scan(&decision.OriginTask,&decision.OriginRun);err!=nil{return err}
  if decision.OriginTask==uuid.Nil||decision.OriginRun==uuid.Nil{return v2input.ErrExpired}
  return tx.Commit(bounded)
 }();if err!=nil{return zero,err}
 prepared,err:=v2optical.LoadNegativeReplacementInputs(ctx,l.DB,l.Reader,l.Bucket,who,c.Tenant,d.Project,decision);if err!=nil{return zero,err}
 snapshot,err:=prepared.ReadOriginalReplacementSnapshot(ctx,l.DB,d.Replacement.OriginalTask);if err!=nil{return zero,err};if snapshot!=c.Snapshot{return zero,v2input.ErrExpired}
 rebuilt,retained,err:=prepared.LoadAdoptedReplacementParents(ctx,l.DB,l.Reader);if err!=nil{return zero,err}
 mappings,unchanged,err:=prepared.LoadMappedReadySources(ctx,l.DB,l.Reader,rebuilt,retained);if err!=nil{return zero,err}
 ready,err:=prepared.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return zero,err}
 var plan PerspectiveReplacementDetectionPlan;matches:=0
 for _,step:=range ready{
  if step.Node().OriginalTask!=d.Replacement.OriginalTask{continue}
  matches++;plan,err=RestorePerspectiveReplacementDetectionPlan(ctx,step,c.Descriptor);if err!=nil{return zero,err}
 }
 if matches!=1{return zero,v2input.ErrExpired}
 if err=ctx.Err();err!=nil{return zero,err}
 return ReplacementDetectionDocuments{DB:l.DB,Bucket:l.Bucket,Prepared:prepared,Plan:plan,Snapshot:snapshot,Rebuilt:rebuilt,Retained:retained,Mappings:mappings,Unchanged:unchanged},nil
}
