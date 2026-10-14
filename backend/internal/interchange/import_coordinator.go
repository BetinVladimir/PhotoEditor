package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/canonical"
)

type ImportExecutionStage interface{Prepare(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask,[]byte)error}
type ImportQualificationStage interface{Prepare(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask)(QualifiedImportActivation,error)}
type ImportActivationStage interface{ActivateImport(context.Context,QualifiedImportActivation)error}
type ImportActivationRecovery interface{RecoverImportActivation(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask,[]byte)(bool,error)}
type ImportExecutionCoordinator struct{Execution ImportExecutionStage;Qualification ImportQualificationStage;Activation ImportActivationStage;Recovery ImportActivationRecovery}

func cloneImportPreparedDocuments(d ImportPreparedDocuments)ImportPreparedDocuments{
 d.plan.Body=append([]byte(nil),d.plan.Body...);d.params.Body=append([]byte(nil),d.params.Body...)
 if d.evidence!=nil{e:=*d.evidence;e.Body=append([]byte(nil),e.Body...);d.evidence=&e}
 return d
}
// Publication and physical IO finish before SQL activation. Errors, including
// ambiguous activation commits, are returned unchanged and never imply success.
// Recovery of an already activated continuation is a separate authority path.
func(c ImportExecutionCoordinator)Execute(ctx context.Context,tenant uuid.UUID,documents ImportPreparedDocuments,task v2input.DraftTask,parsedRaw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if tenant==uuid.Nil||documents.evidence==nil||c.Execution==nil||c.Qualification==nil||c.Activation==nil{return v2input.ErrInvalid}
 documents=cloneImportPreparedDocuments(documents);task.ExecutionProfile=append(json.RawMessage(nil),task.ExecutionProfile...)
 if err:=validateImportTask(documents,task);err!=nil{return err}
 parsed:=append([]byte(nil),parsedRaw...)
 if _,err:=validateParsedImportDocument(ctx,parsed);err!=nil{return err}
 var params importParamsDocument
 if err:=decodeImportDocument(documents.params.Body,&params);err!=nil{return err}
 if params.Parsed.SHA256!=rawSHA(parsed)||params.Plan!=documents.plan.Ref||params.Evidence==nil||*params.Evidence!=documents.evidence.Ref{return v2input.ErrInvalid}
 ownTask:=func()v2input.DraftTask{copy:=task;copy.ExecutionProfile=append(json.RawMessage(nil),task.ExecutionProfile...);return copy}
 recover:=func()(bool,error){
  if c.Recovery==nil{return false,nil}
  if err:=ctx.Err();err!=nil{return false,err}
  return c.Recovery.RecoverImportActivation(ctx,tenant,cloneImportPreparedDocuments(documents),ownTask(),append([]byte(nil),parsed...))
 }
 if recovered,err:=recover();err!=nil{return err}else if recovered{return nil}
 if err:=c.Execution.Prepare(ctx,tenant,cloneImportPreparedDocuments(documents),ownTask(),append([]byte(nil),parsed...));err!=nil{
  // Another continuation may have committed after the initial receipt read.
  if recovered,recoveryErr:=recover();recoveryErr==nil&&recovered{return nil};return err
 }
 if err:=ctx.Err();err!=nil{return err}
 qualified,err:=c.Qualification.Prepare(ctx,tenant,cloneImportPreparedDocuments(documents),ownTask());if err!=nil{return err}
 if err:=ctx.Err();err!=nil{return err}
 if qualified.tenant!=tenant||qualified.digest!=task.InputDigest||qualified.documents.identity!=documents.identity||qualified.task.ID!=task.ID{return v2input.ErrInvalid}
 expected,actual:=documents.Documents(),qualified.documents.Documents()
 if len(expected)!=len(actual){return v2input.ErrInvalid}
 for i:=range expected{if expected[i].Purpose!=actual[i].Purpose||expected[i].Ref!=actual[i].Ref||!bytes.Equal(expected[i].Body,actual[i].Body){return v2input.ErrInvalid}}
 // All scalar task fields as well as the exact measured profile must match.
 got,err:=canonical.Marshal(qualified.task);if err!=nil{return err}
 want,err:=canonical.Marshal(task);if err!=nil{return err}
 if !bytes.Equal(got,want)||!bytes.Equal(qualified.task.ExecutionProfile,task.ExecutionProfile){return v2input.ErrInvalid}
 if err:=c.Activation.ActivateImport(ctx,qualified);err!=nil{
  if recovered,recoveryErr:=recover();recoveryErr==nil&&recovered{return nil};return err
 }
 return nil
}

func NewSQLImportExecutionCoordinator(ctx context.Context,tenant uuid.UUID,documents ImportPreparedDocuments,parsed []byte,deps ImportResolverDependencies,objects ExportExecutionObjects,resources *MeasuredImportResourceQualifier)(ImportExecutionCoordinator,error){
 var zero ImportExecutionCoordinator
 if err:=ctx.Err();err!=nil{return zero,err}
 if resources==nil{return zero,v2input.ErrInvalid}
 execution,err:=NewSQLImportExecutionPreparation(ctx,tenant,documents,parsed,deps,objects);if err!=nil{return zero,err}
 deps.Objects=objects
 qualification,err:=NewSQLImportActivationPreparation(ctx,tenant,documents,parsed,deps,resources);if err!=nil{return zero,err}
 activator:=SQLImportActivator{DB:deps.DB,Tenant:tenant,Parsed:append([]byte(nil),parsed...)}
 return ImportExecutionCoordinator{Execution:execution,Qualification:qualification,Activation:activator,Recovery:activator},nil
}
