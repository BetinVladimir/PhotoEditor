package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
)

// SQLReviewDocumentRepository implements the shared reserve/PUT/complete publisher.
// Intent is frozen before construction; the SQL authority is rechecked on both IO sides.
type SQLReviewDocumentRepository struct {DB *pgxpool.Pool;Intent FrozenReviewIntent;Run uuid.UUID;Bucket string}
func(s SQLReviewDocumentRepository)Scope()artifactjson.Scope{return artifactjson.Scope{Tenant:s.Intent.tenant,Run:s.Run,Owner:s.Intent.owner,Purpose:"review_selection"}}
func(s SQLReviewDocumentRepository)valid(scope artifactjson.Scope,sha string,size int64)error{
 if s.DB==nil||s.Run==uuid.Nil||s.Intent.workflow==uuid.Nil||scope!=s.Scope()||sha!=s.Intent.documentSHA||size!=int64(len(s.Intent.document))||len(s.Bucket)<3||len(s.Bucket)>63{return objectstore.ErrInvalid};return nil
}
func(s SQLReviewDocumentRepository)reportSHA()string{var document struct{SHA string `json:"report_sha"`};_ =json.Unmarshal(s.Intent.document,&document);return document.SHA}
func(s SQLReviewDocumentRepository)checkRun(ctx context.Context,tx pgx.Tx)error{
 var run uuid.UUID
 if err:=tx.QueryRow(ctx,`SELECT run_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2`,s.Intent.tenant,s.Intent.workflow).Scan(&run);err!=nil{return err}
 if run!=s.Run{return objectstore.ErrIntegrity};return nil
}
func(s SQLReviewDocumentRepository)Reserve(ctx context.Context,scope artifactjson.Scope,sha string,size int64)(artifactjson.Reservation,error){
 if err:=ctx.Err();err!=nil{return artifactjson.Reservation{},err};if err:=s.valid(scope,sha,size);err!=nil{return artifactjson.Reservation{},err}
 var result artifactjson.Reservation
 err:=database.TenantTransaction(ctx,s.DB,scope.Tenant,func(tx pgx.Tx)error{
  if err:=s.checkRun(ctx,tx);err!=nil{return err}
  var outcome string;var id,token *uuid.UUID;var ready bool
  err:=tx.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_interchange_review_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,scope.Tenant,s.Intent.workflow,s.Intent.actor.UserID,s.Intent.actor.SessionID,s.Intent.revision,s.Intent.action,s.reportSHA(),s.Intent.requestSHA,sha,size,s.Bucket).Scan(&outcome,&id,&token,&ready);if err!=nil{return err}
  switch outcome{case "quota":return artifactjson.ErrQuota;case "conflict":return artifactjson.ErrConflict;case "created","resumed","replayed":default:return artifactjson.ErrStale}
  if id==nil||*id==uuid.Nil||token==nil||*token==uuid.Nil||ready!=(outcome=="replayed"){return objectstore.ErrIntegrity}
  result=artifactjson.Reservation{ArtifactID:*id,Token:*token};if ready{result.Ready=&artifactjson.Ref{ID:*id,SHA256:sha}};return nil
 });return result,err
}
func(s SQLReviewDocumentRepository)Complete(ctx context.Context,scope artifactjson.Scope,reservation artifactjson.Reservation,object objectstore.Object)(artifactjson.Ref,error){
 if err:=ctx.Err();err!=nil{return artifactjson.Ref{},err};if err:=s.valid(scope,object.SHA256,object.Bytes);err!=nil{return artifactjson.Ref{},err}
 key,err:=artifactjson.DocumentKey(scope,reservation.ArtifactID)
 if err!=nil||reservation.ArtifactID==uuid.Nil||reservation.Token==uuid.Nil||object.Key!=key||object.ContentType!="application/json"||object.VersionID==""||object.VersionID=="null"{return artifactjson.Ref{},objectstore.ErrInvalid}
 err=database.TenantTransaction(ctx,s.DB,scope.Tenant,func(tx pgx.Tx)error{
  if err:=s.checkRun(ctx,tx);err!=nil{return err};var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.complete_v30_interchange_review_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,scope.Tenant,s.Intent.workflow,s.Intent.actor.UserID,s.Intent.actor.SessionID,s.Intent.revision,s.Intent.action,s.reportSHA(),s.Intent.requestSHA,reservation.ArtifactID,reservation.Token,object.SHA256,object.Bytes,s.Bucket,object.VersionID).Scan(&accepted);err!=nil{return err}
  if !accepted{return artifactjson.ErrStale};return nil
 });if err!=nil{return artifactjson.Ref{},err};return artifactjson.Ref{ID:reservation.ArtifactID,SHA256:object.SHA256},nil
}
