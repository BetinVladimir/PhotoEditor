package interchange

import (
 "context"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportDocumentPublisher interface{PublishExportDocument(context.Context,CreateIdentity,CreateDocument)(v2input.Ref,error)}
type ExportTaskMaterializer interface{MaterializeExport(context.Context,CreateIntent,FrozenExportDocuments,v2input.DraftTask)error}

// PrepareExportTask runs after draft creation commits and before transport IO.
// Documents may remain for retry if materialization fails; no runnable task is
// returned and the SQL materializer must repeat ledger/ACL/quota qualification.
func PrepareExportTask(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,inventory ExportInputInventory,resources ExportTaskResources,publisher ExportDocumentPublisher,materializer ExportTaskMaterializer)(v2input.DraftTask,error){
 var zero v2input.DraftTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if publisher==nil||materializer==nil{return zero,v2input.ErrInvalid}
 intent.RequestJCS=append([]byte(nil),intent.RequestJCS...)
 if intent.Request.SnapshotID!=nil{id:=*intent.Request.SnapshotID;intent.Request.SnapshotID=&id}
 task,err:=CompileExportTask(ctx,intent,frozen,inventory,resources);if err!=nil{return zero,err}
 checked,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return zero,err}
 for _,doc:=range []CreateDocument{{Purpose:"v30_interchange_export_plan",Ref:compiled.planRef,Body:append([]byte(nil),compiled.planBody...)},{Purpose:"task_params",Ref:compiled.paramsRef,Body:append([]byte(nil),compiled.paramsBody...)}}{
  expected:=doc.Ref
  if err:=ctx.Err();err!=nil{return zero,err}
  receipt,err:=publisher.PublishExportDocument(ctx,intent.Identity,doc);if err!=nil{return zero,err}
  if receipt!=expected{return zero,objectstore.ErrIntegrity}
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 owned:=task;owned.ExecutionProfile=append([]byte(nil),task.ExecutionProfile...)
 if err:=materializer.MaterializeExport(ctx,copyExportIntent(intent),checked,owned);err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return task,nil
}
