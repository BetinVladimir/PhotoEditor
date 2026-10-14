package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type ExportFrozenProposalSource interface{
 ProposeExportDocuments(context.Context,auth.Identity,uuid.UUID,CreateRequest,string)(FrozenExportDocuments,error)
}
type ExportDraftPreparer interface{
 PrepareExportDraft(context.Context,auth.Identity,uuid.UUID,CreateRequest,string,FrozenExportDocuments)(FrozenExportDocuments,CompiledExportPlan,error)
}
type ExportExecutionInstallation struct{
 Execution ExportExecutionPreparation
 Activation ExportActivationPreparation
 Resources ExportTaskResources
}
// Installation binds every IO adapter to the committed ledger's exact scope.
type ExportExecutionInstaller interface{
 InstallExportExecution(context.Context,auth.Identity,uuid.UUID,CreateRequest,string,FrozenExportDocuments)(ExportExecutionInstallation,error)
}
type ExportCreationPreparation struct{
 Proposals ExportFrozenProposalSource
 Drafts ExportDraftPreparer
 Installation ExportExecutionInstaller
}

// Implements ExportCreatePreparer. Database draft commit finishes before any
// task-document/transport publication. Stored recovery evidence wins over the
// proposal. No graph is sealed until the caller activates the returned receipt.
func(p ExportCreationPreparation)PrepareExportCreation(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string)(PreparedExportActivation,error){
 var zero PreparedExportActivation
 if err:=ctx.Err();err!=nil{return zero,err}
 if p.Proposals==nil||p.Drafts==nil||p.Installation==nil{return zero,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,err}
 if intent.Request.Direction!="export"{return zero,v2input.ErrInvalid}
 proposed,err:=p.Proposals.ProposeExportDocuments(ctx,who,tid,copyExportIntent(intent).Request,key);if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,proposed.Body());err!=nil{return zero,err}
 committed,_,err:=p.Drafts.PrepareExportDraft(ctx,who,tid,copyExportIntent(intent).Request,key,FrozenExportDocuments{body:proposed.Body(),identity:proposed.identity});if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,committed.Body());if err!=nil{return zero,err}
 installation,err:=p.Installation.InstallExportExecution(ctx,who,tid,copyExportIntent(intent).Request,key,FrozenExportDocuments{body:checked.Body(),identity:checked.identity});if err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 // Refuse incomplete installation before beginning object publication.
 execution:=installation.Execution
 if execution.Documents==nil||execution.Resolver==nil||execution.Materializer==nil||execution.TransportDocuments==nil||execution.Registrar==nil||installation.Activation.Resolver==nil||installation.Activation.Resources==nil{return zero,v2input.ErrInvalid}
 execution.Qualification=installation.Activation.Resources
 transport,err:=execution.Prepare(ctx,who,tid,copyExportIntent(intent).Request,key,checked,installation.Resources);if err!=nil{return zero,err}
 expected,err:=CompileExportTask(ctx,intent,checked,transport.inventory,installation.Resources);if err!=nil{return zero,err}
 return installation.Activation.Prepare(ctx,intent,checked,expected)
}
var _ ExportCreatePreparer=ExportCreationPreparation{}
var _ ExportDraftPreparer=Service{}
