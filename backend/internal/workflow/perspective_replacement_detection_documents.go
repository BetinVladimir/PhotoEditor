package workflow
import (
 "context"
 "bytes"
 "photoeditor/backend/internal/canonical"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)
type ReplacementDetectionDocuments struct {
 DB v2optical.NegativeTransactions
 Bucket string
 Prepared v2optical.PreparedNegativeReplacementInputs
 Plan PerspectiveReplacementDetectionPlan
 Snapshot uuid.UUID
 Rebuilt,Retained []v2optical.VerifiedAdoptedParentArrays
 Mappings []v2optical.VerifiedNegativeAffectedSources
 Unchanged []v2optical.VerifiedNegativeUnchangedArrays
}
var _ artifactjson.Repository=ReplacementDetectionDocuments{}
func(r ReplacementDetectionDocuments) validate(ctx context.Context,s artifactjson.Scope,sha string,size int64)(PreparedTask,error){
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return PreparedTask{},err}
 if r.DB==nil||r.Snapshot==uuid.Nil||len(r.Bucket)<3||len(r.Bucket)>63||s!=scope||sha!=task.Params.SHA256||size!=int64(len(body)){return PreparedTask{},v2input.ErrInvalid}
 return task,nil
}
func(r ReplacementDetectionDocuments) transaction(ctx context.Context,scope artifactjson.Scope,apply func(context.Context,pgx.Tx,[]byte)error)error{
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 if err:=ctx.Err();err!=nil{return err}
 tx,err:=r.DB.Begin(ctx);if err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",scope.Tenant.String());err!=nil{return err}
 step,proof,err:=r.Prepared.PerspectiveDetectionPublicationProof(ctx,tx,r.Rebuilt,r.Retained,r.Mappings,r.Unchanged,r.Plan.Descriptor(),r.Snapshot);if err!=nil{return err}
 if _,err=RestorePerspectiveReplacementDetectionPlan(ctx,step,r.Plan.Descriptor());err!=nil{return err}
 if err=apply(ctx,tx,proof);err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 return tx.Commit(ctx)
}
func(r ReplacementDetectionDocuments) Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var zero artifactjson.Reservation
 task,err:=r.validate(ctx,s,sha,size);if err!=nil{return zero,err}
 result:=zero
 err=r.transaction(ctx,s,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var outcome string;var id,token *uuid.UUID;var ready bool
  err:=tx.QueryRow(ctx,"SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_replacement_detector_params_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)",s.Tenant,s.Run,s.Owner,s.Purpose,sha,size,r.Bucket,string(proof)).Scan(&outcome,&id,&token,&ready);if err!=nil{return err}
  switch outcome{case "quota":return artifactjson.ErrQuota;case "conflict":return artifactjson.ErrConflict;case "forbidden","stale":return artifactjson.ErrStale;case "created","resumed","replayed":default:return objectstore.ErrIntegrity}
  if id==nil||*id!=task.Params.ID||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return objectstore.ErrIntegrity}
  result=artifactjson.Reservation{ArtifactID:*id,Token:*token}
  if ready{result.Ready=&artifactjson.Ref{ID:*id,SHA256:sha}}
  return nil
 });if err!=nil{return zero,err};return result,nil
}
func(r ReplacementDetectionDocuments) Complete(ctx context.Context,s artifactjson.Scope,ticket artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 task,err:=r.validate(ctx,s,obj.SHA256,obj.Bytes);if err!=nil{return zero,err}
 key,err:=artifactjson.DocumentKey(s,ticket.ArtifactID)
 if err!=nil||ticket.ArtifactID!=task.Params.ID||ticket.Token==uuid.Nil||obj.Key!=key||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"||len(obj.VersionID)>1024{return zero,objectstore.ErrIntegrity}
 err=r.transaction(ctx,s,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var completed bool
  err:=tx.QueryRow(ctx,"SELECT photo.complete_v29_replacement_detector_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)",s.Tenant,s.Run,s.Owner,s.Purpose,ticket.ArtifactID,ticket.Token,obj.SHA256,obj.Bytes,r.Bucket,obj.VersionID,string(proof)).Scan(&completed)
  if err!=nil{return err};if !completed{return artifactjson.ErrStale};return nil
 });if err!=nil{return zero,err}
 return artifactjson.Ref{ID:ticket.ArtifactID,SHA256:obj.SHA256},nil
}
func(r ReplacementDetectionDocuments) Publish(ctx context.Context,objects artifactjson.Objects)(PreparedTask,error){
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return PreparedTask{},err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return PreparedTask{},err}
 if objects==nil{return PreparedTask{},v2input.ErrInvalid}
 if err=r.ReserveOutput(ctx);err!=nil{return PreparedTask{},err}
 ref,err:=(artifactjson.Publisher{Repository:r,Objects:objects}).Publish(ctx,scope,body);if err!=nil{return PreparedTask{},err}
 if ref.ID!=task.Params.ID||ref.SHA256!=task.Params.SHA256{return PreparedTask{},objectstore.ErrIntegrity}
 return task,ctx.Err()
}

// ReserveOutput retains a bounded tenant storage reservation independently of
// document accounting. Recovered expired/conflicting reservations are rejected.
func(r ReplacementDetectionDocuments) ReserveOutput(ctx context.Context)error{
 scope,body,task,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return err}
 return r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var accepted bool
  err:=tx.QueryRow(ctx,"SELECT photo.reserve_v29_replacement_detector_output($1,$2,$3,$4::jsonb)",scope.Tenant,scope.Run,scope.Owner,string(proof)).Scan(&accepted)
  if err!=nil{return err};if !accepted{return artifactjson.ErrStale};return nil
 })
}

// Stage persists the exact compiled blocked task. No queue/event/outbox/seal.
func(r ReplacementDetectionDocuments) Stage(ctx context.Context,task PreparedTask)error{
 scope,body,expected,_,err:=r.Plan.PreparePublication(ctx);if err!=nil{return err}
 if _,err=r.validate(ctx,scope,expected.Params.SHA256,int64(len(body)));err!=nil{return err}
 actual,err:=canonical.Marshal(task);if err!=nil{return err}
 exact,err:=canonical.Marshal(expected);if err!=nil{return err}
 if !bytes.Equal(actual,exact){return v2input.ErrInvalid}
 return r.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var staged bool
  err:=tx.QueryRow(ctx,"SELECT photo.materialize_v29_replacement_detector_task($1,$2,$3::jsonb,$4::jsonb)",scope.Tenant,scope.Run,string(actual),string(proof)).Scan(&staged)
  if err!=nil{return err};if !staged{return artifactjson.ErrStale};return nil
 })
}
