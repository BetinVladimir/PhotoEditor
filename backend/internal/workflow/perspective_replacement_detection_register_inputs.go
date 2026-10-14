package workflow
import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
)
// RegisterInputs records the complete typed closure after stream verification.
// Full mapped physical locks and current locator comparisons precede mutation.
// No graph seal, ready transition, outbox event or dispatch is performed.
func(r ReplacementDetectionDocuments) RegisterInputs(ctx context.Context,closure PerspectiveLinesClosure,verified VerifiedReplacementDetectionInputs)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 admission,err:=closure.AdmissionJSON(r.Plan.detector,task.Params);if err!=nil{return err}
 witness,err:=verified.AdmissionJSON(ctx,r.Plan,r.Snapshot,r.Bucket);if err!=nil{return err}
 return r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var accepted bool
  err:=tx.QueryRow(ctx,"SELECT photo.register_v29_replacement_detector_inputs($1,$2,$3::jsonb,$4::jsonb,$5::jsonb)",scope.Tenant,scope.Run,string(proof),string(admission),string(witness)).Scan(&accepted)
  if err!=nil{return err};if !accepted{return artifactjson.ErrStale};return nil
 })
}
