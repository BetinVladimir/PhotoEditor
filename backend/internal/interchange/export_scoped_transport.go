package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// PublishScopedExportInputs must follow committed blocked-task materialization.
// The shared document publisher enforces physical reservation/run/task scope;
// this binding additionally prevents a caller from publishing another frozen
// request's inventory. This function neither registers nor seals the task.
func PublishScopedExportInputs(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,frozen FrozenExportDocuments,inventory ExportInputInventory,documents v2input.DocumentPublisher,assemblyLease uuid.UUID)(ExportTransport,error){
 if err:=ctx.Err();err!=nil{return ExportTransport{},err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return ExportTransport{},err}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return ExportTransport{},err}
 if documents==nil||inventory.task!=compiled.task{return ExportTransport{},v2input.ErrInvalid}
 seen:=make(map[uuid.UUID]v2input.Entry,len(inventory.entries))
 for _,entry:=range inventory.entries{
  if _,exists:=seen[entry.Ref.ArtifactID];exists{return ExportTransport{},v2input.ErrInvalid}
  seen[entry.Ref.ArtifactID]=entry
 }
 for _,root:=range compiled.roots{
  if seen[root.Ref.ArtifactID]!=root{return ExportTransport{},v2input.ErrInvalid}
 }
 return inventory.PublishInputs(ctx,v2input.ScopedPublisher{Documents:documents,Tenant:tid,Run:intent.Identity.Run,AssemblyLease:assemblyLease})
}
