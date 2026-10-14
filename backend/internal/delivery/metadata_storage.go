package delivery

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type MetadataDocumentRepository struct {
 DB ProofTransaction
 Scope MetadataProofScope
 Plan MetadataWritePlan
 Bucket string
 transport map[string]metadataTransportBody
}
func(s MetadataDocumentRepository)document(scope artifactjson.Scope)(v2input.Ref,[]byte,error){
 if s.DB==nil||s.Scope.Tenant==uuid.Nil||s.Scope.Expansion==uuid.Nil||s.Scope.Token==uuid.Nil||
 scope.Tenant!=s.Scope.Tenant||scope.Run!=s.Scope.ChildRun||scope.Owner!=s.Scope.Task||scope.AssemblyLease!=s.Scope.Token||
 scope.LocalPortrait||scope.DonorPortrait||len(s.Bucket)<3||len(s.Bucket)>63{return v2input.Ref{},nil,objectstore.ErrInvalid}
 workflow,err:=v2input.WorkflowID(s.Scope.ChildRun)
 if err!=nil{return v2input.Ref{},nil,err}
 task,err:=v2input.TaskID(workflow,"metadata.write",0)
 if err!=nil||task!=s.Scope.Task{return v2input.Ref{},nil,objectstore.ErrInvalid}
 var ref v2input.Ref;var body []byte
 switch scope.Purpose{
 case "task_params":ref=s.Plan.Params();body=s.Plan.ParamsJCS()
 case "v30_metadata_projection":ref=s.Plan.Projection();body=s.Plan.ProjectionJCS()
 default:
  document,ok:=s.transport[scope.Purpose]
  if !ok{return v2input.Ref{},nil,objectstore.ErrInvalid}
  ref=document.ref;body=append([]byte(nil),document.body...)
 }
 if ref.ArtifactID==uuid.Nil||len(body)<2||digest(body)!=ref.SHA256||scope.Purpose=="task_params"&&len(body)>65536||len(body)>1048576{return v2input.Ref{},nil,objectstore.ErrInvalid}
 return ref,body,nil
}
func(s MetadataDocumentRepository)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var zero artifactjson.Reservation
 if err:=ctx.Err();err!=nil{return zero,err}
 ref,body,err:=s.document(scope)
 if err!=nil{return zero,err}
 if sha!=ref.SHA256||size!=int64(len(body)){return zero,objectstore.ErrInvalid}
 var outcome string;var id,token *uuid.UUID;var ready bool
 err=s.DB.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_metadata_document($1,$2,$3,$4,$5,$6)`,
 s.Scope.Tenant,s.Scope.Expansion,s.Scope.Token,scope.Purpose,string(body),s.Bucket).Scan(&outcome,&id,&token,&ready)
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 switch outcome{case "conflict":return zero,artifactjson.ErrConflict;case "stale","forbidden":return zero,artifactjson.ErrStale;case "quota":return zero,artifactjson.ErrQuota;case "created","resumed","replayed":default:return zero,objectstore.ErrIntegrity}
 if id==nil||*id!=ref.ArtifactID||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return zero,objectstore.ErrIntegrity}
 result:=artifactjson.Reservation{ArtifactID:*id,Token:*token}
 if ready{result.Ready=&artifactjson.Ref{ID:*id,SHA256:ref.SHA256}}
 return result,nil
}
func(s MetadataDocumentRepository)Complete(ctx context.Context,scope artifactjson.Scope,reservation artifactjson.Reservation,object objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 if err:=ctx.Err();err!=nil{return zero,err}
 ref,body,err:=s.document(scope)
 if err!=nil{return zero,err}
 key,err:=objectstore.DocumentKey(scope.Tenant,scope.Run,ref.ArtifactID)
 if err!=nil||reservation.ArtifactID!=ref.ArtifactID||reservation.Token==uuid.Nil||reservation.Ready!=nil||
 object.Key!=key||object.SHA256!=ref.SHA256||object.Bytes!=int64(len(body))||object.ContentType!="application/json"||!objectstore.ValidInterchangeVersionID(object.VersionID){return zero,objectstore.ErrInvalid}
 var accepted bool
 err=s.DB.QueryRow(ctx,`SELECT photo.complete_v30_metadata_document($1,$2,$3,$4,$5,$6,$7,$8)`,
 s.Scope.Tenant,s.Scope.Expansion,s.Scope.Token,scope.Purpose,string(body),s.Bucket,reservation.Token,object.VersionID).Scan(&accepted)
 if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 if !accepted{return zero,artifactjson.ErrStale}
 return artifactjson.Ref{ID:ref.ArtifactID,SHA256:ref.SHA256},nil
}

// PublishMetadataDocuments performs independent SQL calls around immutable IO;
// DB must be a tenant-scoped scheduler connection, never an open transaction.
func PublishMetadataDocuments(ctx context.Context,repository MetadataDocumentRepository,objects artifactjson.Objects)error{
 publisher:=artifactjson.Publisher{Repository:repository,Objects:objects}
 for _,purpose:=range []string{"v30_metadata_projection","task_params"}{
  if err:=ctx.Err();err!=nil{return err}
  scope:=metadataPublicationScope(repository.Scope,purpose)
  ref,body,err:=repository.document(scope)
  if err!=nil{return err}
  actual,err:=publisher.Publish(ctx,scope,body)
  if err!=nil{return err}
  if actual.ID!=ref.ArtifactID||actual.SHA256!=ref.SHA256{return objectstore.ErrIntegrity}
 }
 return ctx.Err()
}


func metadataPublicationScope(scope MetadataProofScope,purpose string)artifactjson.Scope{
 return artifactjson.Scope{Tenant:scope.Tenant,Run:scope.ChildRun,Owner:scope.Task,AssemblyLease:scope.Token,Purpose:purpose}
}
