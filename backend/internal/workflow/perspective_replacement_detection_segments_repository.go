package workflow

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type replacementSegmentsRepository struct { source ReplacementDetectionDocuments; plan ReplacementDetectionSegmentsPlan }
var _ artifactjson.Repository=replacementSegmentsRepository{}
func(r replacementSegmentsRepository) validate(s artifactjson.Scope,sha string,size int64)error{
 if s!=r.plan.scope||sha!=r.plan.sha||size!=int64(len(r.plan.body))||r.plan.artifact==uuid.Nil||len(r.plan.body)<2||len(r.plan.body)>1<<20{return v2input.ErrInvalid};return nil
}
func(r replacementSegmentsRepository) lockedProof(ctx context.Context,tx pgx.Tx)([]byte,error){
 if _,err:=r.source.LockSegmentsPlan(ctx,tx,r.plan);err!=nil{return nil,err}
 _,proof,err:=r.source.Prepared.PerspectiveDetectionCurrentProof(ctx,tx,r.source.Rebuilt,r.source.Retained,r.source.Mappings,r.source.Unchanged,r.source.Plan.Descriptor(),r.source.Snapshot);if err!=nil{return nil,err}
 return canonical.Marshal(map[string]any{"detection":json.RawMessage(proof),"result_id":r.plan.result,"root":r.plan.detectorRoot,"segments_jcs":r.plan.body})
}
func(r replacementSegmentsRepository) transaction(ctx context.Context,apply func(context.Context,pgx.Tx,[]byte)error)error{
 return r.source.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  proof,err:=r.lockedProof(ctx,tx);if err!=nil{return err};return apply(ctx,tx,proof)
 })
}
func(r replacementSegmentsRepository) Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var out artifactjson.Reservation
 if err:=r.validate(s,sha,size);err!=nil{return out,err}
 err:=r.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var outcome string;var id,token *uuid.UUID;var ready bool
  if err:=tx.QueryRow(ctx,"SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_replacement_segments_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)",s.Tenant,s.Run,s.Owner,s.Purpose,sha,size,r.source.Bucket,string(proof)).Scan(&outcome,&id,&token,&ready);err!=nil{return err}
  switch outcome{case "quota":return artifactjson.ErrQuota;case "conflict":return artifactjson.ErrConflict;case "forbidden","stale":return artifactjson.ErrStale;case "created","resumed","replayed":default:return objectstore.ErrIntegrity}
  if id==nil||*id!=r.plan.artifact||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return objectstore.ErrIntegrity}
  out=artifactjson.Reservation{ArtifactID:*id,Token:*token};if ready{out.Ready=&artifactjson.Ref{ID:*id,SHA256:sha}};return nil
 });if err!=nil{return artifactjson.Reservation{},err};return out,nil
}
func(r replacementSegmentsRepository) Complete(ctx context.Context,s artifactjson.Scope,ticket artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 if err:=r.validate(s,obj.SHA256,obj.Bytes);err!=nil{return zero,err}
 key,err:=artifactjson.DocumentKey(s,ticket.ArtifactID)
 if err!=nil||ticket.ArtifactID!=r.plan.artifact||ticket.Token==uuid.Nil||obj.Key!=key||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"||len(obj.VersionID)>1024{return zero,objectstore.ErrIntegrity}
 err=r.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var done bool
  err:=tx.QueryRow(ctx,"SELECT photo.complete_v29_replacement_segments_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)",s.Tenant,s.Run,s.Owner,s.Purpose,ticket.ArtifactID,ticket.Token,obj.SHA256,obj.Bytes,r.source.Bucket,obj.VersionID,string(proof)).Scan(&done)
  if err!=nil{return err};if !done{return artifactjson.ErrStale};return nil
 });if err!=nil{return zero,err};return artifactjson.Ref{ID:ticket.ArtifactID,SHA256:obj.SHA256},nil
}
func(r ReplacementDetectionDocuments) PublishSegments(ctx context.Context,objects artifactjson.Objects)(ReplacementDetectionSegmentsPlan,error){
 var zero ReplacementDetectionSegmentsPlan
 if objects==nil{return zero,v2input.ErrInvalid}
 plan,err:=r.PrepareSegmentsPlan(ctx);if err!=nil{return zero,err}
 ref,err:=(artifactjson.Publisher{Repository:replacementSegmentsRepository{source:r,plan:plan},Objects:objects}).Publish(ctx,plan.Scope(),plan.Bytes())
 if err!=nil{return zero,err};if ref.ID!=plan.artifact||ref.SHA256!=plan.sha{return zero,objectstore.ErrIntegrity}
 return plan,ctx.Err()
}
