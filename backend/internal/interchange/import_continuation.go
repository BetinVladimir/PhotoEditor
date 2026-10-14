package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Scheduler-owned continuation after terminal canonical ingest and committed
// draft metadata. All identity, snapshot, limits and operator dependencies are
// fixed at construction; callers cannot supply an execution profile or task.
type SQLImportContinuation struct{
 tenant uuid.UUID
 identity ImportDocumentIdentity
 project uuid.UUID
 parsedRef v2input.Ref
 parsed []byte
 snapshot string
 limits ImportPlanLimits
 deps ImportResolverDependencies
 objects ExportExecutionObjects
 recipes ImportPreparedRecipeResolver
 resources *MeasuredImportResourceQualifier
}
func NewSQLImportContinuation(ctx context.Context,tenant uuid.UUID,identity ImportDocumentIdentity,project uuid.UUID,parsedRef v2input.Ref,parsedRaw []byte,snapshot string,limits ImportPlanLimits,deps ImportResolverDependencies,objects ExportExecutionObjects,recipes ImportPreparedRecipeResolver,resources *MeasuredImportResourceQualifier)(*SQLImportContinuation,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if tenant==uuid.Nil||identity.Run==uuid.Nil||identity.Workflow==uuid.Nil||project==uuid.Nil||objects==nil||resources==nil||recipes==nil||deps.DB==nil||!validImportRef(parsedRef)||rawSHA(parsedRaw)!=parsedRef.SHA256||!validImportRefForSHA(snapshot){return nil,v2input.ErrInvalid}
 if limits.ArchiveBytes<1||limits.ArchiveBytes>64<<30||limits.ExpandedBytes<1||limits.ExpandedBytes>128<<30||limits.Files<1||limits.Files>30000{return nil,v2input.ErrInvalid}
 parsed:=append([]byte(nil),parsedRaw...)
 deps.Objects=objects
 if provider,ok:=recipes.(interface{PreparedImportRegistry()*ImportPreparedNodeRegistry});ok{deps.Prepared=provider.PreparedImportRegistry()}
 if _,err:=NewSQLImportExecutionCompiler(ctx,tenant,identity.Workflow,project,parsedRef,parsed,deps,recipes);err!=nil{return nil,err}
 // Validate the complete optional native warp installation before any writes.
 if _,err:=NewSQLImportMasterReader(tenant,identity.Workflow,parsed,deps);err!=nil{return nil,err}
 if deps.MaxFileBytes<1||deps.MaxFileBytes>128<<30{return nil,v2input.ErrInvalid}
 if deps.WarpScratch!=nil||deps.WarpProbe!=nil||deps.WarpRuntimeSHA!=""{
  if deps.WarpScratch==nil||deps.WarpProbe==nil||!validImportRefForSHA(deps.WarpRuntimeSHA){return nil,v2input.ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 return &SQLImportContinuation{tenant:tenant,identity:identity,project:project,parsedRef:parsedRef,parsed:parsed,snapshot:snapshot,limits:limits,deps:deps,objects:objects,recipes:recipes,resources:resources},nil
}

// ContinueImport returns only after activation or exact historical admission
// recovery. It does not promise worker success, adoption or review completion.
// All object IO is outside SQL transactions; partial immutable publication can
// be retried using existing scoped reservation receipts.
func(s *SQLImportContinuation)ContinueImport(ctx context.Context)error{
 if err:=ctx.Err();err!=nil{return err}
 if s==nil||s.resources==nil||s.objects==nil{return v2input.ErrInvalid}
 parsed:=append([]byte(nil),s.parsed...)
 compiler,err:=NewSQLImportExecutionCompiler(ctx,s.tenant,s.identity.Workflow,s.project,s.parsedRef,parsed,s.deps,s.recipes);if err!=nil{return err}
 documents,err:=compiler.Compile(ctx,s.identity,s.parsedRef,parsed,s.snapshot,s.limits);if err!=nil{return err}
 execution,err:=NewSQLImportExecutionPreparation(ctx,s.tenant,documents,parsed,s.deps,s.objects);if err!=nil{return err}
 // Actual SQL/object readers can only resolve roots once they are published.
 // This stage still creates neither a task nor a resource reservation.
 if _,err:=documents.Publish(ctx,execution.Documents);err!=nil{return err}
 inventory,err:=ResolveImportInputInventory(ctx,s.tenant,documents,execution.Resolver);if err!=nil{return err}
 task,err:=s.resources.BuildImportTask(ctx,documents,inventory);if err!=nil{return err}
 coordinator,err:=NewSQLImportExecutionCoordinator(ctx,s.tenant,documents,parsed,s.deps,s.objects,s.resources);if err!=nil{return err}
 return coordinator.Execute(ctx,s.tenant,documents,task,parsed)
}
