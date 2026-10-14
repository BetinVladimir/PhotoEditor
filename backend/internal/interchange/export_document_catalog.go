package interchange

import (
 "context"
 "errors"
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

type SQLExportOutputCatalog struct{DB *pgxpool.Pool;Tenant uuid.UUID;Who auth.Identity;Request CreateRequest;Key,Bucket string;Frozen FrozenExportDocuments}

// LookupExportOutput authorizes only canonical succeeded task receipts; unlike
// input publication scope, it never accepts merely project-linked artifacts.
func(s SQLExportOutputCatalog)LookupExportOutput(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 switch entry.Contract{case "InterchangeResult","MappingReport","MappingShard","InterchangeArchive":default:return objectstore.Object{},v2input.ErrInvalid}
 return lookupExportCatalog(ctx,s.DB,s.Tenant,s.Who,s.Request,s.Key,s.Bucket,s.Frozen,entry,true)
}

type SQLExportDocumentCatalog struct{DB *pgxpool.Pool;Tenant uuid.UUID;Who auth.Identity;Request CreateRequest;Key,Bucket string;Frozen FrozenExportDocuments}
func(s SQLExportDocumentCatalog)LookupExportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 return lookupExportCatalog(ctx,s.DB,s.Tenant,s.Who,s.Request,s.Key,s.Bucket,s.Frozen,entry,false)
}

func lookupExportCatalog(ctx context.Context,db *pgxpool.Pool,tid uuid.UUID,who auth.Identity,request CreateRequest,key,bucketName string,frozen FrozenExportDocuments,entry v2input.Entry,output bool)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 if db==nil||len(bucketName)<3||len(bucketName)>63||!validImportRef(entry.Ref){return objectstore.Object{},v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return objectstore.Object{},err}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());err!=nil{return objectstore.Object{},err}
 var object objectstore.Object
 err=tenant.WithTenant(ctx,db,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  query:=`SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_export_document($1,$2,$3,$4,$5,$6,$7,$8)`
  if output{query=`SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_export_output($1,$2,$3,$4,$5,$6,$7,$8)`}
  err:=tx.QueryRow(ctx,query,tid,who.UserID,who.SessionID,key,intent.RequestSHA,entry.Ref.ArtifactID,entry.Ref.SHA256,entry.Contract).Scan(&bucket,&object.Key,&object.VersionID,&object.SHA256,&object.Bytes,&object.ContentType)
  if err!=nil{return err}
  if bucket!=bucketName||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=entry.Ref.SHA256||object.Bytes<1||object.Bytes>128<<30{return objectstore.ErrIntegrity}
  return nil
 })
 if errors.Is(err,pgx.ErrNoRows){return objectstore.Object{},artifactjson.ErrStale}
 if err!=nil{return objectstore.Object{},err}
 return object,ctx.Err()
}
