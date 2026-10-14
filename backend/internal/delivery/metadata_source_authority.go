package delivery

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AuthorizeMetadataSourcePublication must use a short tenant-scoped photo_api
// transaction. It performs no object IO and does not reserve or publish. The
// caller supplies authenticated scope and approval, not receipt-selected values.
func AuthorizeMetadataSourcePublication(ctx context.Context,db ProofTransaction,
 scope MetadataSourcePublicationScope,plan MetadataSourcePublication,policy string,approved []string)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil{return v2input.ErrInvalid}
 owned,err:=RestoreMetadataSourcePublication(plan.JCS(),plan.SHA256(),scope,plan.Ref(),plan.SourceJCS(),policy,approved)
 if err!=nil{return err}
 scopeRaw,err:=canonical.Marshal(scope);if err!=nil{return err}
 refRaw,err:=canonical.Marshal(owned.Ref());if err!=nil{return err}
 var allowed bool
 err=db.QueryRow(ctx,`SELECT photo.v30_metadata_source_authorized($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
 scope.Tenant,scope.Project,scope.Actor,scope.Session,string(scopeRaw),string(owned.JCS()),owned.SHA256(),
 string(refRaw),string(owned.SourceJCS()),policy,append([]string{},approved...)).Scan(&allowed)
 if err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 if !allowed{return v2input.ErrExpired}
 return nil
}
