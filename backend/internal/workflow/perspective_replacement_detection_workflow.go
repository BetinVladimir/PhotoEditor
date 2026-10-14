package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

// CreateDraftWorkflow freezes operator execution pins for the exact ready
// transport. This does not attest source object IO or activate the detector.
func(r ReplacementDetectionDocuments) CreateDraftWorkflow(ctx context.Context,policy PerspectiveLinesExecutionPolicy,closure PerspectiveLinesClosure)(uuid.UUID,error){
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return uuid.Nil,err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return uuid.Nil,err}
 raw,err:=policy.ForPlan(r.Plan.detector);if err!=nil{return uuid.Nil,err}
 admission,err:=closure.AdmissionJSON(r.Plan.detector,task.Params);if err!=nil{return uuid.Nil,err}
 expected,err:=v2input.WorkflowID(scope.Run);if err!=nil{return uuid.Nil,err}
 err=r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var accepted *bool
  if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_replacement_detector_transport_admission($1,$2,$3,$4::jsonb)=$5::jsonb",scope.Tenant,scope.Run,scope.Owner,string(proof),string(admission)).Scan(&accepted);err!=nil{return err}
  if accepted==nil||!*accepted{return artifactjson.ErrStale}
  var saved *uuid.UUID
  if err:=tx.QueryRow(ctx,"SELECT photo.create_v29_replacement_detector_workflow($1,$2,$3,$4::jsonb)",scope.Tenant,scope.Run,string(raw),string(proof)).Scan(&saved);err!=nil{return err}
  if saved==nil||*saved!=expected{return artifactjson.ErrStale};return nil
 });if err!=nil{return uuid.Nil,err};return expected,nil
}
