package workflow
import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
)
// Activate admits the initial graph only after fresh stream witnesses and full
// mapped physical source locks. Ambiguous committed retries need admission
// recovery; a draft read cannot acknowledge an already active run.
func(r ReplacementDetectionDocuments) Activate(ctx context.Context,verified VerifiedReplacementDetectionInputs)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 witness,err:=verified.AdmissionJSON(ctx,r.Plan,r.Snapshot,r.Bucket);if err!=nil{return err}
 return r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var accepted bool
  err:=tx.QueryRow(ctx,"SELECT photo.activate_v29_replacement_detector($1,$2,$3::jsonb,$4::jsonb)",scope.Tenant,scope.Run,string(proof),string(witness)).Scan(&accepted)
  if err!=nil{return err};if !accepted{return artifactjson.ErrStale};return nil
 })
}
