package workflow

import (
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// CompileDeliveryWatermarkNode compiles only after the prior presentation result
// is adopted. Caller must prove child/parent/preset and persisted pin authority;
// this value compiler never invents a future resize/sharpen output reference.
func CompileDeliveryWatermarkNode(child uuid.UUID,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,configRaw []byte,imageRef v2input.Ref,imageRaw []byte,registry Registry,limits DeliveryDerivativeLimits)(CompiledDownloadNode,json.RawMessage,error){
 var zero CompiledDownloadNode
 op,ok:=registry["derivative.watermark"]
 if child==uuid.Nil||!ok||op.Name!="derivative.watermark"||op.Version!="1"||op.ResourceClass!="cpu"||op.Model!=nil||op.TimeoutSeconds<=0{return zero,nil,domain.NewError("DELIVERY_WATERMARK_UNAVAILABLE",503)}
 cpu:=false;for _,backend:=range op.Backends{cpu=cpu||backend=="cpu"};if !cpu{return zero,nil,domain.NewError("DELIVERY_WATERMARK_UNAVAILABLE",503)}
 stage,err:=pin.CompileStage(scope,configRaw,imageRef,imageRaw);if err!=nil{return zero,nil,err}
 estimate,err:=compileDeliveryWatermarkEstimate(stage.ImagePixels,stage.WatermarkPixels,stage.ImageHasAlpha,limits);if err!=nil{return zero,nil,err}
 profile,err:=canonical.Marshal(map[string]any{"backend":"cpu","renderer_version":"v2.2","resource_estimate":estimate});if err!=nil{return zero,nil,err}
 workflow,err:=v2input.WorkflowID(child);if err!=nil{return zero,nil,err}
 task,err:=v2input.TaskID(workflow,op.Name,0);if err!=nil{return zero,nil,err}
 key,err:=v2input.NodeKey(op.Name,0);if err!=nil{return zero,nil,err}
 inputs:=make([]review.ArtifactRef,len(stage.Inputs));for n,ref:=range stage.Inputs{inputs[n]=review.ArtifactRef{ID:ref.ArtifactID,SHA256:ref.SHA256}}
 return CompiledDownloadNode{ID:task,Ordinal:0,Key:key,Operation:op.Name,Version:op.Version,ResourceClass:op.ResourceClass,TimeoutSeconds:op.TimeoutSeconds,Inputs:inputs,Params:stage.Params},profile,nil
}

func compileDeliveryWatermarkEstimate(imagePixels,markPixels int64,alpha bool,limits DeliveryDerivativeLimits)(ResourceEstimate,error){
 var zero ResourceEstimate
 if imagePixels<1||imagePixels>200000000||markPixels<1||markPixels>200000000{return zero,domain.NewError("DELIVERY_RESOURCE_INVALID",422)}
 channels:=int64(12);if alpha{channels=16}
 // Includes full source/mark linear and premultiplied buffers, compositing
 // temporaries and immutable staged output. Qualification must measure peaks.
 estimate:=ResourceEstimate{RAMBytes:(256<<20)+192*(imagePixels+markPixels),VRAMBytes:0,ScratchBytes:(32<<20)+channels*imagePixels+16*markPixels,OutputBytes:(8<<20)+channels*imagePixels,PixelCount:imagePixels}
 if limits.RAMBytes<estimate.RAMBytes||limits.ScratchBytes<estimate.ScratchBytes||limits.OutputBytes<estimate.OutputBytes{return zero,domain.NewError("DELIVERY_RESOURCE_INVALID",422)}
 return estimate,nil
}
