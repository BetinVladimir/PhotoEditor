package workflow
import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
)
// SealTransport freezes the full typed closure into worker wire inputs/profile.
// It leaves task blocked and run draft. Durable retry/activation is separate.
func(r ReplacementDetectionDocuments) SealTransport(ctx context.Context,closure PerspectiveLinesClosure,verified VerifiedReplacementDetectionInputs)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 admission,err:=closure.AdmissionJSON(r.Plan.detector,task.Params);if err!=nil{return err}
 witness,err:=verified.AdmissionJSON(ctx,r.Plan,r.Snapshot,r.Bucket);if err!=nil{return err}
 return r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var accepted bool
  err:=tx.QueryRow(ctx,"SELECT photo.seal_v29_replacement_detector_transport($1,$2,$3::jsonb,$4::jsonb,$5::jsonb)",scope.Tenant,scope.Run,string(proof),string(admission),string(witness)).Scan(&accepted)
  if err!=nil{return err};if !accepted{return artifactjson.ErrStale};return nil
 })
}
