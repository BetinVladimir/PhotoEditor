package workflow

import (
 "context"
 "bytes"
 "photoeditor/backend/internal/exportpreset"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

type FrozenDeliveryWatermarkTask struct {task FrozenDeliveryInitialTask; child uuid.UUID; pin delivery.FrozenWatermarkSource; scope delivery.WatermarkPinScope; config []byte; imageRef v2input.Ref; image []byte}
// FreezeDeliveryWatermarkTask pins a real adopted image and watermark source,
// complete caller-resolved inventory, exact recipe/preset roots and CPU profile.
// It does not establish predecessor adoption or grant task activation authority.
func FreezeDeliveryWatermarkTask(child uuid.UUID,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,configRaw []byte,imageRef v2input.Ref,imageRaw []byte,recipe,preset,paramsRef v2input.Ref,resolved []v2input.Entry,registry Registry,limits DeliveryDerivativeLimits)(FrozenDeliveryWatermarkTask,error){
 var zero FrozenDeliveryWatermarkTask
 if preset.SHA256!=scope.PresetSHA{return zero,v2input.ErrInvalid}
 node,profile,err:=CompileDeliveryWatermarkNode(child,pin,scope,configRaw,imageRef,imageRaw,registry,limits);if err!=nil{return zero,err}
 stage,err:=pin.CompileStage(scope,configRaw,imageRef,imageRaw);if err!=nil{return zero,err}
 roots:=make([]v2input.Entry,0,len(stage.Inputs)+2)
 markOffset:=2;if stage.ImageHasAlpha{markOffset=3}
 for n,ref:=range stage.Inputs{contract:="NpyArray";if n==0||n==markOffset{contract="WorkingImage"};roots=append(roots,v2input.Entry{Ref:ref,Contract:contract})}
 roots=append(roots,v2input.Entry{Ref:recipe,Contract:"EditingRecipeV3"},v2input.Entry{Ref:preset,Contract:"ExportPresetConfig"})
 workflow,err:=v2input.WorkflowID(child);if err!=nil{return zero,err}
 closure,err:=v2input.CompileDeliveryWatermarkClosure(workflow,node.Params,paramsRef,roots,resolved);if err!=nil{return zero,err}
 draft:=v2input.DraftTask{ID:node.ID,NodeKey:node.Key,Operation:node.Operation,OperationVersion:node.Version,ResourceClass:node.ResourceClass,ParamsArtifactID:paramsRef.ArtifactID,ParamsSHA:paramsRef.SHA256,InputDigest:closure.InputDigest,ExecutionProfile:profile}
 return FrozenDeliveryWatermarkTask{task:FrozenDeliveryInitialTask{draft:draft,closure:closure},child:child,pin:pin,scope:scope,config:append([]byte(nil),configRaw...),imageRef:imageRef,image:append([]byte(nil),imageRaw...)},nil
}
func(f FrozenDeliveryWatermarkTask)DraftTask()(v2input.DraftTask,error){return f.task.DraftTask()}
func(f FrozenDeliveryWatermarkTask)PublishInputs(ctx context.Context,publisher v2input.Publisher)(v2input.GraphTask,error){return f.task.PublishInputs(ctx,publisher)}

// InitialRequest binds the owned initial-stage descriptor to its prepare child.
// This proves immutable values only; SQL must recheck current source authority.
func(f FrozenDeliveryWatermarkTask)InitialRequest(lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent)(delivery.FrozenExportRequest,[]byte,error){
 var zero delivery.FrozenExportRequest
 if _,err:=f.DraftTask();err!=nil{return zero,nil,err}
 if lease.Tenant!=f.scope.Tenant||lease.Run!=f.scope.Parent||child.RunID!=f.child||lease.ParentTask!=plan.TaskID(){return zero,nil,v2input.ErrInvalid}
 request,raw,err:=plan.WatermarkExportRequest(lease.Run,child,f.scope,f.pin)
 if err!=nil{return zero,nil,err}
 if request.Binding.Source!=f.imageRef||!bytes.Equal([]byte(request.ConfigJCS),f.config){return zero,nil,v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig(f.config,f.scope.PresetSHA)
 if err!=nil||config.Size!=nil||config.Sharpen!=nil{return zero,nil,v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(child.RunID);if err!=nil{return zero,nil,err}
 id,err:=v2input.TaskID(workflow,f.task.draft.Operation,0)
 if err!=nil||id!=f.task.draft.ID||f.task.draft.Operation!="derivative.watermark"{return zero,nil,v2input.ErrInvalid}
 return request,raw,nil
}
