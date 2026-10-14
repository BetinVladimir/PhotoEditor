package interchange

import (
 "bytes"
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type SQLImportApprovalCatalog struct{
 DB *pgxpool.Pool
 Scope ImportPreparedScope
 Who auth.Identity
 Revision int64
 ReportSHA string
 Parsed []byte
 Bucket string
}
func(s SQLImportApprovalCatalog)LookupImportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 switch entry.Contract{case "StyleV2","CorrectionPlan","Override","LayerStack","MaskRef","Params":default:return objectstore.Object{},v2input.ErrInvalid}
 if s.DB==nil||s.Scope.Tenant==uuid.Nil||s.Scope.Run==uuid.Nil||s.Scope.Workflow==uuid.Nil||s.Scope.Entry==uuid.Nil||s.Who.UserID==uuid.Nil||s.Who.SessionID==uuid.Nil||s.Revision<1||s.Revision>9007199254740990||!validImportRef(entry.Ref)||!validImportRef(v2input.Ref{ArtifactID:s.Scope.Entry,SHA256:s.ReportSHA})||len(s.Bucket)<3||len(s.Bucket)>63||len(s.Parsed)<2||len(s.Parsed)>8<<20{return objectstore.Object{},v2input.ErrInvalid}
 parsed:=append([]byte(nil),s.Parsed...);normalized,err:=canonical.Bytes(parsed);if err!=nil||!bytes.Equal(parsed,normalized){return objectstore.Object{},v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(ctx,parsed);err!=nil{return objectstore.Object{},err}
 binding,err:=canonical.Marshal(s.Scope.Binding);if err!=nil{return objectstore.Object{},err}
 var object objectstore.Object
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Scope.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  row:=tx.QueryRow(ctx,`SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_approval_document($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12)`,s.Scope.Tenant,s.Who.UserID,s.Who.SessionID,s.Scope.Workflow,s.Revision,s.ReportSHA,string(parsed),s.Scope.Entry,string(binding),entry.Ref.ArtifactID,entry.Ref.SHA256,entry.Contract)
  var err error;object,err=scanPreparedImportObject(row,s.Scope,entry.Ref,s.Bucket,"application/json",8<<20);return err
 })
 if errors.Is(err,pgx.ErrNoRows){return objectstore.Object{},artifactjson.ErrStale}
 if err!=nil{return objectstore.Object{},err};return object,ctx.Err()
}
