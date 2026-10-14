package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

type automaticPerspectiveParamsRepository struct {
 source ReplacementDetectionDocuments
 segments ReadyReplacementSegments
 located v2optical.LocatedPerspectiveQualification
 locks v2qualification.DeploymentLocks
 maxPixels int64
 capacity v2optical.PerspectiveResourceEstimate
 draft v2optical.PerspectiveReplacementDraft
}
func(r automaticPerspectiveParamsRepository) document()(artifactjson.Scope,uuid.UUID,[]byte,string,error){
 var packet struct{Identity v2optical.NegativeReplacementPublicationIdentity `json:"identity"`;Draft v2optical.PerspectiveDraftSnapshot `json:"draft"`}
 if len(r.draft.Bytes())<2||json.Unmarshal(r.draft.Bytes(),&packet)!=nil{return artifactjson.Scope{},uuid.Nil,nil,"",v2input.ErrInvalid}
 d:=packet.Draft;i:=packet.Identity
 if i.Tenant==uuid.Nil||i.Run==uuid.Nil||i.Task==uuid.Nil||i.ParamsDocument==uuid.Nil||d.Run!=i.Run||d.Task!=i.Task||d.ParamsDocument!=i.ParamsDocument||d.ParamsSHA!=i.ParamsSHA||renderDerivativeBytesSHA([]byte(d.ParamsJCS))!=i.ParamsSHA{return artifactjson.Scope{},uuid.Nil,nil,"",v2input.ErrInvalid}
 return artifactjson.Scope{Tenant:i.Tenant,Run:i.Run,Owner:i.Task,Purpose:"task_params"},i.ParamsDocument,[]byte(d.ParamsJCS),i.ParamsSHA,nil
}
func(r automaticPerspectiveParamsRepository) validate(s artifactjson.Scope,sha string,size int64)(uuid.UUID,error){
 scope,id,body,expected,err:=r.document();if err!=nil{return uuid.Nil,err}
 if scope!=s||sha!=expected||size!=int64(len(body))||size<2||size>65536{return uuid.Nil,v2input.ErrInvalid};return id,nil
}
func(r automaticPerspectiveParamsRepository) transaction(ctx context.Context,apply func(context.Context,pgx.Tx,[]byte)error)error{
 return r.source.adoptionTransaction(ctx,func(ctx context.Context,tx pgx.Tx)error{
  current,err:=r.source.LockAutomaticPerspectiveDraft(ctx,tx,r.segments,r.located,r.locks,r.maxPixels,r.capacity);if err!=nil{return err}
  if !bytes.Equal(current.Bytes(),r.draft.Bytes()){return v2input.ErrExpired}
  segments,err:=(replacementSegmentsRepository{source:r.source,plan:r.segments.plan}).lockedProof(ctx,tx);if err!=nil{return err}
  proof,err:=canonical.Marshal(map[string]any{"bucket":r.source.Bucket,"draft_jcs":string(current.Bytes()),"segments":json.RawMessage(segments)});if err!=nil{return err}
  return apply(ctx,tx,proof)
 })
}
func(r automaticPerspectiveParamsRepository) Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var out artifactjson.Reservation
 expected,err:=r.validate(s,sha,size);if err!=nil{return out,err}
 err=r.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var outcome string;var id,token *uuid.UUID;var ready bool
  err:=tx.QueryRow(ctx,"SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_auto_perspective_params_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb)",s.Tenant,s.Run,s.Owner,s.Purpose,sha,size,r.source.Bucket,string(proof)).Scan(&outcome,&id,&token,&ready);if err!=nil{return err}
  switch outcome{case "quota":return artifactjson.ErrQuota;case "conflict":return artifactjson.ErrConflict;case "forbidden","stale":return artifactjson.ErrStale;case "created","resumed","replayed":default:return objectstore.ErrIntegrity}
  if id==nil||*id!=expected||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return objectstore.ErrIntegrity}
  out=artifactjson.Reservation{ArtifactID:*id,Token:*token};if ready{out.Ready=&artifactjson.Ref{ID:*id,SHA256:sha}};return nil
 });if err!=nil{return artifactjson.Reservation{},err};return out,nil
}
func(r automaticPerspectiveParamsRepository) Complete(ctx context.Context,s artifactjson.Scope,ticket artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 expected,err:=r.validate(s,obj.SHA256,obj.Bytes);if err!=nil{return zero,err}
 key,err:=artifactjson.DocumentKey(s,ticket.ArtifactID)
 if err!=nil||ticket.ArtifactID!=expected||ticket.Token==uuid.Nil||obj.Key!=key||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"||len(obj.VersionID)>1024{return zero,objectstore.ErrIntegrity}
 err=r.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var done bool
  err:=tx.QueryRow(ctx,"SELECT photo.complete_v29_auto_perspective_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)",s.Tenant,s.Run,s.Owner,s.Purpose,ticket.ArtifactID,ticket.Token,obj.SHA256,obj.Bytes,r.source.Bucket,obj.VersionID,string(proof)).Scan(&done)
  if err!=nil{return err};if !done{return artifactjson.ErrStale};return nil
 });if err!=nil{return zero,err};return artifactjson.Ref{ID:ticket.ArtifactID,SHA256:obj.SHA256},nil
}
var _ artifactjson.Repository=automaticPerspectiveParamsRepository{}
func(r ReplacementDetectionDocuments) PublishAutomaticPerspectiveParams(ctx context.Context,objects artifactjson.Objects,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2optical.PerspectiveReplacementDraft,error){
 var zero v2optical.PerspectiveReplacementDraft
 if objects==nil{return zero,v2input.ErrInvalid}
 draft,err:=r.LoadAutomaticPerspectiveDraft(ctx,segments,located,locks,maxPixels,capacity);if err!=nil{return zero,err}
 repo:=automaticPerspectiveParamsRepository{source:r,segments:segments,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,draft:draft}
 scope,id,body,sha,err:=repo.document();if err!=nil{return zero,err}
 ref,err:=(artifactjson.Publisher{Repository:repo,Objects:objects}).Publish(ctx,scope,body)
 if err!=nil{return zero,err};if ref.ID!=id||ref.SHA256!=sha{return zero,objectstore.ErrIntegrity}
 return draft,ctx.Err()
}

// StageAutomaticPerspectiveTask creates a blocked shell and live output quota
// only. Dedicated transport registration and graph seal must follow separately.
func(r ReplacementDetectionDocuments) StageAutomaticPerspectiveTask(ctx context.Context,draft v2optical.PerspectiveReplacementDraft,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)error{
 if err:=ctx.Err();err!=nil{return err}
 repo:=automaticPerspectiveParamsRepository{source:r,segments:segments,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,draft:draft}
 scope,_,_,_,err:=repo.document();if err!=nil{return err}
 descriptor,err:=draft.AutomaticStageDescriptor(ctx,located);if err!=nil{return err}
 return repo.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var staged bool
  err:=tx.QueryRow(ctx,"SELECT photo.stage_v29_auto_perspective_replacement_task($1,$2,$3,$4::jsonb)",scope.Tenant,scope.Run,string(descriptor),string(proof)).Scan(&staged)
  if err!=nil{return err};if !staged{return v2input.ErrExpired};return nil
 })
}

func(r ReplacementDetectionDocuments) PublishAutomaticPerspectiveTransport(ctx context.Context,objects artifactjson.Objects,draft v2optical.PerspectiveReplacementDraft,segments ReadyReplacementSegments,located v2optical.LocatedPerspectiveQualification,locks v2qualification.DeploymentLocks,maxPixels int64,capacity v2optical.PerspectiveResourceEstimate)(v2input.GraphTask,error){
 var zero v2input.GraphTask
 if err:=ctx.Err();err!=nil{return zero,err};if objects==nil{return zero,v2input.ErrInvalid}
 if err:=r.StageAutomaticPerspectiveTask(ctx,draft,segments,located,locks,maxPixels,capacity);err!=nil{return zero,err}
 repo:=automaticPerspectiveParamsRepository{source:r,segments:segments,located:located,locks:locks,maxPixels:maxPixels,capacity:capacity,draft:draft}
 scope,_,_,_,err:=repo.document();if err!=nil{return zero,err}
 publisher:=v2input.ScopedPublisher{Documents:automaticPerspectiveTransportDocuments{params:repo,objects:objects},Tenant:scope.Tenant,Run:scope.Run}
 return draft.BuildAutomaticTransport(ctx,located,publisher)
}
