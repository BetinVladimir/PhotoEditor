package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type automaticPerspectiveTransportRepository struct {params automaticPerspectiveParamsRepository;body string}
func(r automaticPerspectiveTransportRepository) validate(scope artifactjson.Scope,sha string,size int64)error{
 base,_,_,_,err:=r.params.document();if err!=nil{return err}
 if scope.Purpose!="v2_input_chunk_0"&&scope.Purpose!="v2_input_manifest"{return v2input.ErrInvalid}
 base.Purpose=scope.Purpose
 if scope!=base||size!=int64(len(r.body))||size<2||size>1<<20||sha!=renderDerivativeBytesSHA([]byte(r.body)){return v2input.ErrInvalid};return nil
}
func(r automaticPerspectiveTransportRepository) Reserve(ctx context.Context,s artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 var out artifactjson.Reservation
 if err:=r.validate(s,sha,size);err!=nil{return out,err}
 err:=r.params.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var outcome string;var id,token *uuid.UUID;var ready bool
  err:=tx.QueryRow(ctx,"SELECT outcome,artifact_id,token,ready FROM photo.reserve_v29_auto_perspective_replacement_transport_document($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)",s.Tenant,s.Run,s.Owner,s.Purpose,sha,size,r.params.source.Bucket,string(proof),r.body).Scan(&outcome,&id,&token,&ready);if err!=nil{return err}
  switch outcome{case "quota":return artifactjson.ErrQuota;case "conflict":return artifactjson.ErrConflict;case "forbidden","stale":return artifactjson.ErrStale;case "created","resumed","replayed":default:return objectstore.ErrIntegrity}
  if id==nil||*id==uuid.Nil||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return objectstore.ErrIntegrity}
  out=artifactjson.Reservation{ArtifactID:*id,Token:*token};if ready{out.Ready=&artifactjson.Ref{ID:*id,SHA256:sha}};return nil
 });if err!=nil{return artifactjson.Reservation{},err};return out,nil
}
func(r automaticPerspectiveTransportRepository) Complete(ctx context.Context,s artifactjson.Scope,ticket artifactjson.Reservation,obj objectstore.Object)(artifactjson.Ref,error){
 var zero artifactjson.Ref
 if err:=r.validate(s,obj.SHA256,obj.Bytes);err!=nil{return zero,err}
 key,err:=artifactjson.DocumentKey(s,ticket.ArtifactID)
 if err!=nil||ticket.Token==uuid.Nil||obj.Key!=key||obj.ContentType!="application/json"||obj.VersionID==""||obj.VersionID=="null"||len(obj.VersionID)>1024{return zero,objectstore.ErrIntegrity}
 err=r.params.transaction(ctx,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var done bool
  err:=tx.QueryRow(ctx,"SELECT photo.complete_v29_auto_perspective_replacement_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12)",s.Tenant,s.Run,s.Owner,s.Purpose,ticket.ArtifactID,ticket.Token,obj.SHA256,obj.Bytes,r.params.source.Bucket,obj.VersionID,string(proof),r.body).Scan(&done)
  if err!=nil{return err};if !done{return artifactjson.ErrStale};return nil
 });if err!=nil{return zero,err};return artifactjson.Ref{ID:ticket.ArtifactID,SHA256:obj.SHA256},nil
}
type automaticPerspectiveTransportDocuments struct {params automaticPerspectiveParamsRepository;objects artifactjson.Objects}
func(d automaticPerspectiveTransportDocuments) Publish(ctx context.Context,s artifactjson.Scope,body []byte)(artifactjson.Ref,error){
 return (artifactjson.Publisher{Repository:automaticPerspectiveTransportRepository{params:d.params,body:string(body)},Objects:d.objects}).Publish(ctx,s,body)
}
var _ artifactjson.Repository=automaticPerspectiveTransportRepository{}
