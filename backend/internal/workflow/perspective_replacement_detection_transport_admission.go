package workflow

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
)

// ConfirmInputClosure compares the entire owned publication result with the
// current immutable ready documents under mapped-source/review/snapshot locks.
// It does not register inputs, seal the graph or authorize worker execution.
func(r ReplacementDetectionDocuments) ConfirmInputClosure(ctx context.Context,closure PerspectiveLinesClosure)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 admission,err:=closure.AdmissionJSON(r.Plan.detector,task.Params);if err!=nil{return err}
 return r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var accepted *bool
  err:=tx.QueryRow(ctx,"SELECT photo.read_v29_replacement_detector_transport_admission($1,$2,$3,$4::jsonb)=$5::jsonb",scope.Tenant,scope.Run,scope.Owner,string(proof),string(admission)).Scan(&accepted)
  if err!=nil{return err};if accepted==nil||!*accepted{return artifactjson.ErrStale};return nil
 })
}
