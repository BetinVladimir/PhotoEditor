package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Installs independent current SQL source and metadata authority around the
// canonical master object reader. Recipe preparation must be explicitly
// supplied: a missing implementation cannot silently erase authored edits.
func NewSQLImportExecutionCompiler(ctx context.Context,tenant,workflow,project uuid.UUID,parsedRef v2input.Ref,parsedRaw []byte,deps ImportResolverDependencies,recipes ImportPreparedRecipeResolver)(ImportExecutionCompiler,error){
 var zero ImportExecutionCompiler
 if err:=ctx.Err();err!=nil{return zero,err}
 if tenant==uuid.Nil||workflow==uuid.Nil||project==uuid.Nil||recipes==nil||!validImportRef(parsedRef)||rawSHA(parsedRaw)!=parsedRef.SHA256{return zero,v2input.ErrInvalid}
 parsed:=append([]byte(nil),parsedRaw...)
 if _,err:=validateParsedImportDocument(ctx,parsed);err!=nil{return zero,err}
 plan,err:=NewSQLImportPlanPreparation(tenant,workflow,project,parsed,deps,recipes);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ImportExecutionCompiler{Plan:plan,Metadata:SQLImportMetadataReader{DB:deps.DB,Tenant:tenant,Workflow:workflow}},nil
}
