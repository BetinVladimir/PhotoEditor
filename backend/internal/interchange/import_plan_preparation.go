package interchange

import (
 "bytes"
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

type ImportSourceReader interface{Read(context.Context,v2input.Ref,[]byte)(ImportSourceInventory,error)}
type ImportPlanPreparation struct{Sources ImportSourceReader;Masters ImportMasterReader;Recipes ImportPreparedRecipeResolver}

// Compile rechecks the complete source snapshot after all compiler object IO.
// Each source read owns a short tenant transaction. Later materialization still
// rechecks authority; this method does not extend an ACL or quota lifetime.
func(p ImportPlanPreparation)Compile(ctx context.Context,parsedRef v2input.Ref,parsedRaw []byte,snapshot string,limits ImportPlanLimits)(CompiledImportPlan,error){
 if err:=ctx.Err();err!=nil{return CompiledImportPlan{},err}
 if p.Sources==nil{return CompiledImportPlan{},v2input.ErrInvalid}
 parsedRaw=append([]byte(nil),parsedRaw...)
 sources,err:=p.Sources.Read(ctx,parsedRef,append([]byte(nil),parsedRaw...));if err!=nil{return CompiledImportPlan{},err}
 compiled,err:=CompileImportPlan(ctx,sources,parsedRaw,snapshot,limits,p.Masters,p.Recipes);if err!=nil{return CompiledImportPlan{},err}
 if err:=ctx.Err();err!=nil{return CompiledImportPlan{},err}
 current,err:=p.Sources.Read(ctx,parsedRef,append([]byte(nil),parsedRaw...));if err!=nil{return CompiledImportPlan{},err}
 if !bytes.Equal(current.body,compiled.sources.body){return CompiledImportPlan{},artifactjson.ErrStale}
 return compiled,ctx.Err()
}

func NewSQLImportPlanPreparation(tenant,workflow,project uuid.UUID,parsedRaw []byte,deps ImportResolverDependencies,recipes ImportPreparedRecipeResolver)(ImportPlanPreparation,error){
 if project==uuid.Nil{return ImportPlanPreparation{},v2input.ErrInvalid}
 master,err:=NewSQLImportMasterReader(tenant,workflow,parsedRaw,deps);if err!=nil{return ImportPlanPreparation{},err}
 return ImportPlanPreparation{Sources:SQLImportSourceReader{DB:deps.DB,Tenant:tenant,Workflow:workflow,Project:project},Masters:master,Recipes:recipes},nil
}
