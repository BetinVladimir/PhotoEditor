package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type SQLImportReviewParsedSource struct{
 DB *pgxpool.Pool
 Tenant,Workflow uuid.UUID
 Who auth.Identity
 Revision int64
 ReportSHA,Bucket string
}
func(s SQLImportReviewParsedSource)ResolveReviewParsed(ctx context.Context)(v2input.Ref,objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return v2input.Ref{},objectstore.Object{},err}
 if s.DB==nil||s.Tenant==uuid.Nil||s.Workflow==uuid.Nil||s.Who.UserID==uuid.Nil||s.Who.SessionID==uuid.Nil||s.Revision<1||s.Revision>9007199254740990||!validImportRef(v2input.Ref{ArtifactID:s.Workflow,SHA256:s.ReportSHA})||len(s.Bucket)<3||len(s.Bucket)>63{return v2input.Ref{},objectstore.Object{},v2input.ErrInvalid}
 var ref v2input.Ref;var object objectstore.Object
 err:=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  rows,err:=tx.Query(ctx,`SELECT artifact_id,bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_review_parsed($1,$2,$3,$4,$5,$6)`,s.Tenant,s.Who.UserID,s.Who.SessionID,s.Workflow,s.Revision,s.ReportSHA);if err!=nil{return err};defer rows.Close()
  if !rows.Next(){if err:=rows.Err();err!=nil{return err};return artifactjson.ErrStale}
  var bucket string
  if err:=rows.Scan(&ref.ArtifactID,&bucket,&object.Key,&object.VersionID,&object.SHA256,&object.Bytes,&object.ContentType);err!=nil{return err}
  if rows.Next(){return v2input.ErrInvalid};if err:=rows.Err();err!=nil{return err}
  ref.SHA256=object.SHA256
  if bucket!=s.Bucket||!validImportRef(ref)||object.Key==""||object.VersionID==""||object.VersionID=="null"||len(object.VersionID)>1024||object.Bytes<2||object.Bytes>8<<20||object.ContentType!="application/json"{return objectstore.ErrIntegrity}
  return nil
 });if err!=nil{return v2input.Ref{},objectstore.Object{},err}
 return ref,object,ctx.Err()
}
