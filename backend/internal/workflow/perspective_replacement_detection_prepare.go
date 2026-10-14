package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

// sealedForActivation retains fresh physical source locks and compares the
// current IO witness/operator policy with the immutable blocked seal. Absence
// means preparation may continue; a conflicting seal is never repaired.
func(r ReplacementDetectionDocuments) sealedForActivation(ctx context.Context,policy PerspectiveLinesExecutionPolicy,verified VerifiedReplacementDetectionInputs)(bool,error){
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return false,err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return false,err}
 rawPolicy,err:=policy.ForPlan(r.Plan.detector);if err!=nil{return false,err}
 witness,err:=verified.AdmissionJSON(ctx,r.Plan,r.Snapshot,r.Bucket);if err!=nil{return false,err}
 sealed:=false
 err=r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var present,exact bool
  err:=tx.QueryRow(ctx,`SELECT r.v29_replacement_detector_seal_receipt IS NOT NULL,
 coalesce(photo.read_v29_replacement_detector_seal_receipt($1,$2,$3)->'witness'=$4::jsonb
 AND r.v29_replacement_detector_execution_policy=$5,false)
 FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`,scope.Tenant,scope.Run,string(r.Plan.Descriptor()),string(witness),string(rawPolicy)).Scan(&present,&exact)
  if err!=nil{return err};if present&&!exact{return artifactjson.ErrStale};sealed=present;return nil
 });if err!=nil{return false,err};return sealed,nil
}

// PrepareAndActivate ensures an exact detector draft after admission probing. Recovery
// probes precede publication; it never recreates a draft or renews expired quota.
// Historical admission acknowledgement is not a result or completion receipt.
// The bool reports recovered existing admission; false means newly activated.
func(r ReplacementDetectionDocuments) PrepareAndActivate(ctx context.Context,policy PerspectiveLinesExecutionPolicy,objects artifactjson.Objects,reader v2input.SessionAnalysisObjectReader)(bool,error){
 if _,_,_,_,err:=r.Plan.PreparePublication(ctx);err!=nil{return false,err}
 if _,err:=policy.ForPlan(r.Plan.detector);err!=nil{return false,err}
 admitted,err:=r.WasAdmitted(ctx);if err!=nil{return false,err};if admitted{return true,nil}
 if objects==nil||reader==nil{return false,v2input.ErrInvalid}
 if err=r.EnsureDraft(ctx);err!=nil{return false,err}
 verified,err:=r.VerifyInputObjects(ctx,reader);if err!=nil{return false,err}
 sealed,err:=r.sealedForActivation(ctx,policy,verified);if err!=nil{return false,err}
 if !sealed{
  task,err:=r.Publish(ctx,objects);if err!=nil{return false,err}
  if err=r.Stage(ctx,task);err!=nil{return false,err}
  closure,err:=r.PublishInputClosure(ctx,objects);if err!=nil{return false,err}
  if _,err=r.CreateDraftWorkflow(ctx,policy,closure);err!=nil{return false,err}
  if err=r.SealTransport(ctx,closure,verified);err!=nil{return false,err}
 }
 if err=r.Activate(ctx,verified);err!=nil{return false,err}
 return false,nil
}
