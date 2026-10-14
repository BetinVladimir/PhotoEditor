package workflow

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type FrozenDeliveryWatermarkEncodeTask struct{task FrozenDeliveryInitialTask;predecessor uuid.UUID;stage int;tenant uuid.UUID;request []byte;image []byte}
// FreezeEncodeAfterWatermark retains source/output/mark/recipe/preset/report
// roots. Caller must resolve the full transitive inventory and SQL must attest
// previous presentation adoption before this descriptor can be admitted.
func(v VerifiedDeliveryWatermarkStage)FreezeEncodeAfterWatermark(request delivery.FrozenExportRequest,targetICC string,registry Registry,limits DeliveryDerivativeLimits,paramsRef v2input.Ref,resolved []v2input.Entry)(FrozenDeliveryWatermarkEncodeTask,error){
 var zero FrozenDeliveryWatermarkEncodeTask
 if request.Preset.Config.SHA256!=v.scope.PresetSHA{return zero,v2input.ErrInvalid}
 node,profile,err:=v.CompileEncodeAfterWatermark(request,targetICC,registry,limits);if err!=nil{return zero,err}
 stage,err:=v.pin.CompileStage(v.scope,v.config,v.sourceRef,v.source);if err!=nil{return zero,err}
 markOffset:=2;if stage.ImageHasAlpha{markOffset=3}
 roots:=make([]v2input.Entry,0,len(stage.Inputs)+8)
 for n,ref:=range stage.Inputs{contract:="NpyArray";if n==0||n==markOffset{contract="WorkingImage"};roots=append(roots,v2input.Entry{Ref:ref,Contract:contract})}
 output,err:=review.ParseWorkingImage(v.output.ImageJSON);if err!=nil{return zero,err}
 roots=append(roots,v2input.Entry{Ref:v2input.Ref{ArtifactID:v.output.ImageRef.ID,SHA256:v.output.ImageRef.SHA256},Contract:"WorkingImage"},v2input.Entry{Ref:v2input.Ref{ArtifactID:output.Pixels.ID,SHA256:output.Pixels.SHA256},Contract:"NpyArray"},v2input.Entry{Ref:v2input.Ref{ArtifactID:v.output.ReportRef.ID,SHA256:v.output.ReportRef.SHA256},Contract:"DerivativeReport"},v2input.Entry{Ref:request.Binding.Source,Contract:"WorkingImage"},v2input.Entry{Ref:request.Recipe,Contract:"EditingRecipeV3"},v2input.Entry{Ref:request.Preset.Config,Contract:"ExportPresetConfig"})
 if output.Alpha!=nil{roots=append(roots,v2input.Entry{Ref:v2input.Ref{ArtifactID:output.Alpha.ID,SHA256:output.Alpha.SHA256},Contract:"NpyArray"})}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return zero,err}
 closure,err:=v2input.CompileDeliveryChildClosure(workflow,node.Operation,node.Params,paramsRef,roots,resolved);if err!=nil{return zero,err}
 config,err:=exportpreset.DecodeFrozenConfig(v.config,v.scope.PresetSHA);if err!=nil{return zero,err}
 index:=1;if config.Size!=nil{index++};if config.Sharpen!=nil{index++}
 draft:=v2input.DraftTask{ID:node.ID,NodeKey:node.Key,Operation:node.Operation,OperationVersion:node.Version,ResourceClass:node.ResourceClass,ParamsArtifactID:paramsRef.ArtifactID,ParamsSHA:paramsRef.SHA256,InputDigest:closure.InputDigest,ExecutionProfile:profile}
 requestRaw,err:=canonical.Marshal(request);if err!=nil{return zero,err}
 return FrozenDeliveryWatermarkEncodeTask{tenant:v.scope.Tenant,request:requestRaw,image:append([]byte(nil),v.output.ImageJSON...),task:FrozenDeliveryInitialTask{draft:draft,closure:closure},predecessor:v.producer.TaskID,stage:index},nil
}
func(f FrozenDeliveryWatermarkEncodeTask)DraftTask()(v2input.DraftTask,error){return f.task.DraftTask()}
func(f FrozenDeliveryWatermarkEncodeTask)Predecessor()uuid.UUID{return f.predecessor}
func(f FrozenDeliveryWatermarkEncodeTask)Stage()int{return f.stage}
func(f FrozenDeliveryWatermarkEncodeTask)PublishInputs(ctx context.Context,publisher v2input.Publisher)(v2input.GraphTask,error){return f.task.PublishInputs(ctx,publisher)}
