package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportInputRegistrar interface{RegisterExportInputs(context.Context,CreateIntent,FrozenExportDocuments,ExportTransport)error}
type ExportExecutionPreparation struct{
 Documents ExportDocumentPublisher
 Resolver ExportClosureNodeResolver
 Materializer ExportTaskMaterializer
 TransportDocuments v2input.DocumentPublisher
 Registrar ExportInputRegistrar
 Qualification ExportResourceQualifier
 AssemblyLease uuid.UUID
}

// Prepare requires a committed private draft run. Each IO stage finishes before
// the next database transaction begins. Failure leaves replayable documents but
// never seals the graph, dispatches the task or returns a public creation ticket.
func(p ExportExecutionPreparation)Prepare(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,frozen FrozenExportDocuments,resources ExportTaskResources)(ExportTransport,error){
 var zero ExportTransport
 if err:=ctx.Err();err!=nil{return zero,err}
 if p.Documents==nil||p.Resolver==nil||p.Materializer==nil||p.TransportDocuments==nil||p.Registrar==nil{return zero,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,err}
 checked,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return zero,err}
 for _,doc:=range []CreateDocument{{Purpose:"v30_interchange_export_plan",Ref:compiled.planRef,Body:append([]byte(nil),compiled.planBody...)},{Purpose:"task_params",Ref:compiled.paramsRef,Body:append([]byte(nil),compiled.paramsBody...)}}{
  expected:=doc.Ref
  receipt,err:=p.Documents.PublishExportDocument(ctx,intent.Identity,doc);if err!=nil{return zero,err}
  if receipt!=expected{return zero,objectstore.ErrIntegrity}
  if err:=ctx.Err();err!=nil{return zero,err}
 }
 inventory,err:=ResolveExportInputInventory(ctx,compiled,p.Resolver);if err!=nil{return zero,err}
 if p.Qualification!=nil{
  measured,err:=p.Qualification.QualifyExportResources(ctx,copyExportIntent(intent),FrozenExportDocuments{body:checked.Body(),identity:checked.identity},ExportInputInventory{task:inventory.task,entries:inventory.Entries(),digest:inventory.digest})
  if err!=nil{return zero,err}
  if measured!=resources{return zero,v2input.ErrInvalid}
  if err:=ctx.Err();err!=nil{return zero,err}
 }
 task,err:=CompileExportTask(ctx,intent,checked,inventory,resources);if err!=nil{return zero,err}
 if err:=p.Materializer.MaterializeExport(ctx,copyExportIntent(intent),checked,task);err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 transport,err:=PublishScopedExportInputs(ctx,who,tid,intent.Request,key,checked,inventory,p.TransportDocuments,p.AssemblyLease);if err!=nil{return zero,err}
 if err:=p.Registrar.RegisterExportInputs(ctx,copyExportIntent(intent),checked,transport);err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return transport,nil
}

type SQLExportInputRegistrar struct{DB *pgxpool.Pool;Tenant uuid.UUID;Who auth.Identity;Request CreateRequest;Key string}
func(s SQLExportInputRegistrar)RegisterExportInputs(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,transport ExportTransport)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil{return v2input.ErrInvalid}
 expected,err:=FreezeCreateIntent(s.Tenant,s.Who,s.Request,s.Key);if err!=nil{return err}
 if intent.Identity!=expected.Identity||intent.RequestSHA!=expected.RequestSHA{return v2input.ErrInvalid}
 return database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{
  return transport.RegisterInputs(ctx,tx,s.Tenant,expected,frozen)
 })
}

// Every callback receives separate public pointer fields and byte buffers.
func copyExportIntent(intent CreateIntent)CreateIntent{
 intent.RequestJCS=append([]byte(nil),intent.RequestJCS...)
 if intent.Request.SnapshotID!=nil{id:=*intent.Request.SnapshotID;intent.Request.SnapshotID=&id}
 if intent.Request.Bundle!=nil{ref:=*intent.Request.Bundle;intent.Request.Bundle=&ref}
 return intent
}
