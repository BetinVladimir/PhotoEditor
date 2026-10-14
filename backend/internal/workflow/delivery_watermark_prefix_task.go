package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// FrozenDeliveryWatermarkPrefixTask owns the next required presentation task.
// Source values have been validated, but producer adoption and physical output
// verification must still be established by the scheduler before SQL admission.
type FrozenDeliveryWatermarkPrefixTask struct{
 task FrozenDeliveryInitialTask
 predecessor uuid.UUID
 stage int
 scope delivery.WatermarkPinScope
 pin delivery.FrozenWatermarkSource
 request []byte
 source []byte
}

func FreezeDeliveryWatermarkPrefixTask(request delivery.FrozenExportRequest,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,source []byte,results []DeliveryPresentationOutput,registry Registry,limits DeliveryDerivativeLimits,params v2input.Ref,resolved []v2input.Entry)(FrozenDeliveryWatermarkPrefixTask,error){
 var zero FrozenDeliveryWatermarkPrefixTask
 next,err:=CompileDeliveryWatermarkPrefix(request,pin,scope,source,results,registry,limits);if err!=nil{return zero,err}
 image,err:=review.ParseWorkingImage(next.SourceJCS);if err!=nil{return zero,err}
 current:=request.Binding.Source
 if len(results)>0{last:=results[len(results)-1];current=v2input.Ref{ArtifactID:last.ImageRef.ID,SHA256:last.ImageRef.SHA256}}
 roots:=[]v2input.Entry{
  {Ref:current,Contract:"WorkingImage"},
  {Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Contract:"NpyArray"},
  {Ref:request.Recipe,Contract:"EditingRecipeV3"},
  {Ref:request.Preset.Config,Contract:"ExportPresetConfig"},
  {Ref:request.Binding.Source,Contract:"WorkingImage"},
 }
 if image.Alpha!=nil{roots=append(roots,v2input.Entry{Ref:v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},Contract:"NpyArray"})}
 for _,result:=range results{
  roots=append(roots,v2input.Entry{Ref:v2input.Ref{ArtifactID:result.ImageRef.ID,SHA256:result.ImageRef.SHA256},Contract:"WorkingImage"},v2input.Entry{Ref:v2input.Ref{ArtifactID:result.ReportRef.ID,SHA256:result.ReportRef.SHA256},Contract:"DerivativeReport"})
 }
 if next.Node.Operation=="derivative.watermark"{
  stage,err:=pin.CompileStage(scope,[]byte(request.ConfigJCS),current,next.SourceJCS);if err!=nil{return zero,err}
  markOffset:=2;if stage.ImageHasAlpha{markOffset=3}
  for n,ref:=range stage.Inputs{contract:="NpyArray";if n==0||n==markOffset{contract="WorkingImage"};roots=append(roots,v2input.Entry{Ref:ref,Contract:contract})}
 }
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return zero,err}
 var closure v2input.CaptureClosure
 if next.Node.Operation=="derivative.watermark"{closure,err=v2input.CompileDeliveryWatermarkClosure(workflow,next.Node.Params,params,roots,resolved)}else{closure,err=v2input.CompileDeliveryChildClosure(workflow,next.Node.Operation,next.Node.Params,params,roots,resolved)}
 if err!=nil{return zero,err}
 draft:=v2input.DraftTask{ID:next.Node.ID,NodeKey:next.Node.Key,Operation:next.Node.Operation,OperationVersion:next.Node.Version,ResourceClass:next.Node.ResourceClass,ParamsArtifactID:params.ArtifactID,ParamsSHA:params.SHA256,InputDigest:closure.InputDigest,ExecutionProfile:next.ExecutionProfile}
 raw,err:=canonical.Marshal(request);if err!=nil{return zero,err}
 return FrozenDeliveryWatermarkPrefixTask{task:FrozenDeliveryInitialTask{draft:draft,closure:closure},predecessor:next.Predecessor,stage:next.Stage,scope:scope,pin:pin,request:raw,source:append([]byte(nil),next.SourceJCS...)},nil
}
func(f FrozenDeliveryWatermarkPrefixTask)DraftTask()(v2input.DraftTask,error){return f.task.DraftTask()}
func(f FrozenDeliveryWatermarkPrefixTask)Predecessor()uuid.UUID{return f.predecessor}
func(f FrozenDeliveryWatermarkPrefixTask)Stage()int{return f.stage}
func(f FrozenDeliveryWatermarkPrefixTask)PublishInputs(ctx context.Context,publisher v2input.Publisher)(v2input.GraphTask,error){return f.task.PublishInputs(ctx,publisher)}
