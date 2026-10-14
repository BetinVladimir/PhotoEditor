package provenance

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type signingNegativeCausalReader interface{
 read(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(signingNegativeCausalReceipt,error)
}
type NegativeSigningTaskResolver struct{Causal signingNegativeCausalReader}

func compileSigningNegativeGraphTask(task CanonicalSigningTask,receipt signingNegativeCausalReceipt)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if normalizeCanonicalSigningTask(&task)!=nil||task.Operation!="optical.negative"||task.Version!="1"{return zero,ErrInvalid}
 evidence:=receipt.evidence
 stamp,err:=compileSigningNegativeEvidenceStamp(task,evidence);if err!=nil||stamp!=evidence.stamp{return zero,ErrInvalid}
 entries,err:=CompileSigningNegativeInputInventory(task,evidence);if err!=nil{return zero,err}
 expectedEntries,err:=canonical.Hash(entries);if err!=nil{return zero,err};actualEntries,err:=canonical.Hash(receipt.entries)
 if err!=nil||expectedEntries!=actualEntries{return zero,ErrInvalid}
 edge:=receipt.pixels
 if edge.input!=evidence.pixels.params.Base.Source||edge.producer==uuid.Nil||edge.producer==task.Task{return zero,ErrInvalid}
 causalStamp,err:=canonical.Hash([]any{stamp,edge.input,edge.producer,entries});if err!=nil||causalStamp!=receipt.stamp{return zero,ErrInvalid}
 outputs,err:=canonicalTaskOutputRefs(task.Outputs)
 if err!=nil||len(outputs)!=1||outputs[0]!=evidence.pixels.root{return zero,ErrInvalid}
 result:=evidence.pixels.result
 report:=evidence.report
 if result.SchemaVersion!=1||result.GeneratedMask!=nil||report.Operation!="optical.negative"||report.Status!="review"||report.Reason==nil||*report.Reason!="negative_review"||len(report.Sources)!=1||report.Sources[0]!=evidence.pixels.params.Base{return zero,ErrInvalid}
 model:=evidence.qualification.ModelLockSHA;review:=evidence.review.ID
 children:=[]SigningChildOutput{{Root:outputs[0],Ref:result.WorkingImage,Role:"working_image"},{Root:outputs[0],Ref:result.ChangedMask,Role:"changed_mask"},{Root:outputs[0],Ref:result.Report,Role:"report"}}
 resolved:=ResolvedSigningTask{Node:SigningExecutionNode{Task:task.Task,Run:task.Run,CanonicalResult:task.Result,Attempt:task.Attempt,Fence:task.Fence,Parents:[]uuid.UUID{edge.producer},Inputs:[]v2input.Ref{edge.input},Outputs:outputs,Children:children,Action:SigningAction{Operation:task.Operation,Version:task.Version,ParamsSHA:task.Params.SHA256,ModelLockSHA:&model,ReviewID:&review,Generated:false}},Sources:[]v2input.Ref{}}
 content,err:=signingNodeContentOutputs(resolved.Node);if err!=nil{return zero,err}
 for _,output:=range content{for _,entry:=range entries{if output.ArtifactID==entry.Ref.ArtifactID{return zero,ErrInvalid}}}
 raw,err:=canonical.Marshal(resolved);if err!=nil||len(raw)>1<<20{return zero,ErrInvalid}
 var owned ResolvedSigningTask;if json.Unmarshal(raw,&owned)!=nil{return zero,ErrInvalid};return owned,nil
}

func(s NegativeSigningTaskResolver)ResolveSigningTaskLineage(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Causal==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||ready.Tenant==uuid.Nil||ready.Project==uuid.Nil||ready.ExportID==uuid.Nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task);if err!=nil{return zero,err}
 if owned.Operation!="optical.negative"||owned.Version!="1"{return zero,ErrInvalid}
 read:=func()(ResolvedSigningTask,string,error){
  if err:=ctx.Err();err!=nil{return zero,"",err}
  isolated,err:=ownSigningProducerTask(owned);if err!=nil{return zero,"",err}
  receipt,err:=s.Causal.read(ctx,who,ready,isolated);if err!=nil{return zero,"",err}
  result,err:=compileSigningNegativeGraphTask(owned,receipt);return result,receipt.stamp,err
 }
 first,stamp,err:=read();if err!=nil{return zero,err}
 current,currentStamp,err:=read();if err!=nil{return zero,err}
 before,err:=canonical.Hash(first);if err!=nil{return zero,err};after,err:=canonical.Hash(current)
 if err!=nil||stamp!=currentStamp||before!=after{return zero,ErrInvalid}
 return current,ctx.Err()
}
var _ SigningTaskLineageResolver=NegativeSigningTaskResolver{}
