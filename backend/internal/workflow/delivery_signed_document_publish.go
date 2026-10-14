package workflow

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type signedDeliveryDocumentRepository struct {
 db artifactjson.RowQuerier
 bucket string
 lease delivery.ExpansionLease
 owner uuid.UUID
 purpose string
 body []byte
}

func (r signedDeliveryDocumentRepository) scope() artifactjson.Scope {
 return artifactjson.Scope{Tenant:r.lease.Tenant,Run:r.lease.Run,Owner:r.owner,
  Purpose:r.purpose,AssemblyLease:r.lease.Token}
}

func (r signedDeliveryDocumentRepository) valid(scope artifactjson.Scope) bool {
 maximum:=65536
 if r.purpose=="v30_delivery_signed_closure" {maximum=8<<20
 } else if r.purpose!="v30_delivery_signed_envelope" {return false}
 return r.db!=nil&&scope==r.scope()&&r.lease.Valid()&&r.lease.Cursor==r.lease.SlotCount&&
  r.owner!=uuid.Nil&&len(r.bucket)>=3&&len(r.bucket)<=63&&len(r.body)>=2&&len(r.body)<=maximum
}

func (r signedDeliveryDocumentRepository) Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if err:=ctx.Err();err!=nil{return artifactjson.Reservation{},err}
 if !r.valid(scope)||sha!=renderDerivativeBytesSHA(r.body)||size!=int64(len(r.body)){
  return artifactjson.Reservation{},objectstore.ErrInvalid
 }
 var outcome string;var id,token *uuid.UUID;var ready bool
 err:=r.db.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_signed_document($1,$2,$3,$4,$5,$6,$7,$8)`,
  r.lease.Expansion,r.lease.Token,r.purpose,string(r.body),r.bucket,scope.Tenant,scope.Run,scope.Owner).
  Scan(&outcome,&id,&token,&ready)
 if err!=nil{return artifactjson.Reservation{},err}
 switch outcome {
 case "conflict":return artifactjson.Reservation{},artifactjson.ErrConflict
 case "forbidden","stale":return artifactjson.Reservation{},artifactjson.ErrStale
 case "quota":return artifactjson.Reservation{},artifactjson.ErrQuota
 case "created","resumed","replayed":
 default:return artifactjson.Reservation{},objectstore.ErrIntegrity
 }
 if id==nil||*id==uuid.Nil||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){
  return artifactjson.Reservation{},objectstore.ErrIntegrity
 }
 ticket:=artifactjson.Reservation{ArtifactID:*id,Token:*token}
 if ready{ticket.Ready=&artifactjson.Ref{ID:*id,SHA256:sha}}
 return ticket,nil
}

func (r signedDeliveryDocumentRepository) Complete(ctx context.Context,scope artifactjson.Scope,
 ticket artifactjson.Reservation,object objectstore.Object)(artifactjson.Ref,error){
 if err:=ctx.Err();err!=nil{return artifactjson.Ref{},err}
 if !r.valid(scope)||ticket.ArtifactID==uuid.Nil||ticket.Token==uuid.Nil||
  object.Bytes!=int64(len(r.body))||object.SHA256!=renderDerivativeBytesSHA(r.body)||
  object.ContentType!="application/json"||object.VersionID==""||object.VersionID=="null"||
  len(object.VersionID)>1024{return artifactjson.Ref{},objectstore.ErrInvalid}
 key,err:=artifactjson.DocumentKey(scope,ticket.ArtifactID)
 if err!=nil||object.Key!=key{return artifactjson.Ref{},objectstore.ErrIntegrity}
 var accepted bool
 err=r.db.QueryRow(ctx,`SELECT photo.complete_v30_delivery_signed_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
  r.lease.Expansion,r.lease.Token,r.purpose,string(r.body),r.bucket,ticket.ArtifactID,
  ticket.Token,object.VersionID,scope.Tenant,scope.Run,scope.Owner).Scan(&accepted)
 if err!=nil{return artifactjson.Ref{},err}
 if !accepted{return artifactjson.Ref{},artifactjson.ErrStale}
 return artifactjson.Ref{ID:ticket.ArtifactID,SHA256:object.SHA256},nil
}

// NewSignedDeliveryDocumentPublisher uses a scheduler-tenant transaction
// RowQuerier. SQL rechecks the retained closure and exact parent lease around
// immutable object IO; this publisher never activates assembly.
func NewSignedDeliveryDocumentPublisher(db artifactjson.RowQuerier,objects artifactjson.Objects,
 bucket string,lease delivery.ExpansionLease,plan delivery.PreparePlan)(delivery.PublishDocument,error){
 workflow,err:=v2input.WorkflowID(lease.Run)
 if err!=nil||db==nil||objects==nil||!lease.Valid()||lease.Workflow!=workflow||
  lease.ParentTask!=plan.TaskID()||lease.Cursor!=lease.SlotCount||len(bucket)<3||len(bucket)>63{
  return nil,v2input.ErrInvalid
 }
 owner,err:=v2input.TaskID(workflow,"v2.delivery.assemble",0)
 if err!=nil{return nil,err}
 return func(ctx context.Context,purpose string,body []byte)(v2input.Ref,error){
  repository:=signedDeliveryDocumentRepository{db:db,bucket:bucket,lease:lease,
   owner:owner,purpose:purpose,body:append([]byte(nil),body...)}
  scope:=repository.scope()
  if !repository.valid(scope){return v2input.Ref{},v2input.ErrInvalid}
  ref,err:=(artifactjson.Publisher{Repository:repository,Objects:objects}).Publish(ctx,scope,repository.body)
  if err!=nil{return v2input.Ref{},err}
  return v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},nil
 },nil
}
