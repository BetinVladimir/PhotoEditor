package workflow

import (
 "fmt"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
// CompileDeliveryWatermarkPrefix replays resize then sharpen on real outputs
// before emitting watermark. It never predicts future artifact refs. This is
// value evidence only: SQL must independently attest producer/fence/adoption.
func CompileDeliveryWatermarkPrefix(request delivery.FrozenExportRequest,pin delivery.FrozenWatermarkSource,scope delivery.WatermarkPinScope,source []byte,results []DeliveryPresentationOutput,registry Registry,limits DeliveryDerivativeLimits)(DeliveryContinuation,error){
 var zero DeliveryContinuation
 if request.SchemaVersion!=1||request.ParentRunID!=scope.Parent||request.SlotIndex<0||request.SlotIndex>=80000||request.ChildRunID!=uuid.NewSHA1(scope.Parent,[]byte(fmt.Sprintf("child:export:%d",request.SlotIndex)))||request.Preset.PresetID!=scope.Preset||request.Preset.PresetSHA!=scope.PresetSHA||request.Preset.Config.SHA256!=scope.PresetSHA||len(results)>2{return zero,v2input.ErrInvalid}
 if _,err:=delivery.DecodeWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,[]byte(request.ConfigJCS));err!=nil{return zero,err}
 pending,err:=compileDeliveryPresentationRequest(request,source,registry);if err!=nil{return zero,err}
 current:=source;ref:=request.Binding.Source;previous:=uuid.Nil
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return zero,err}
 for _,result:=range results{
  if pending==nil{return zero,v2input.ErrInvalid}
  previous,err=v2input.TaskID(workflow,pending.Operation,0);if err!=nil{return zero,err}
  switch pending.Operation{
  case "derivative.resize":
   pending,err=compileDeliveryAfterResizeRequest(request,source,result.ImageRef,result.ImageJSON,result.ReportRef,result.ReportJSON,registry)
  case "derivative.sharpen":
   _,err=ValidateDeliverySharpenResult(*pending,current,result.ImageRef,result.ImageJSON,result.ReportRef,result.ReportJSON);pending=nil
  default:return zero,v2input.ErrInvalid
  }
  if err!=nil{return zero,err};current=result.ImageJSON;ref=v2input.Ref{ArtifactID:result.ImageRef.ID,SHA256:result.ImageRef.SHA256}
 }
 if pending==nil{
  node,profile,err:=CompileDeliveryWatermarkNode(request.ChildRunID,pin,scope,[]byte(request.ConfigJCS),ref,current,registry,limits)
  if err!=nil{return zero,err};return DeliveryContinuation{Node:node,SourceJCS:append([]byte(nil),current...),ExecutionProfile:profile,Predecessor:previous,Stage:len(results)},nil
 }
 node:=*pending
 node.ID,err=v2input.TaskID(workflow,node.Operation,0);if err!=nil{return zero,err}
 node.Key,err=v2input.NodeKey(node.Operation,0);if err!=nil{return zero,err}
 profile,err:=CompileDeliveryDerivativeProfile(node,current,limits);if err!=nil{return zero,err}
 return DeliveryContinuation{Node:node,SourceJCS:append([]byte(nil),current...),ExecutionProfile:profile,Predecessor:previous,Stage:len(results)},nil
}
