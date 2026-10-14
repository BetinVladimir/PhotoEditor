package workflow

import (
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// CompileEncodeAfterWatermark consumes physically checked local evidence. SQL
// must independently require watermark adoption before admitting this task.
func(v VerifiedDeliveryWatermarkStage)CompileEncodeAfterWatermark(request delivery.FrozenExportRequest,targetICC string,registry Registry,limits DeliveryDerivativeLimits)(CompiledDownloadNode,json.RawMessage,error){
 var zero CompiledDownloadNode
 if request.SchemaVersion!=1||request.ParentRunID!=v.scope.Parent||request.ChildRunID==uuid.Nil||request.Preset.PresetID!=v.scope.Preset||request.Preset.PresetSHA!=v.scope.PresetSHA||!bytes.Equal(v.config,[]byte(request.ConfigJCS)){return zero,nil,v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return zero,nil,err}
 predecessor,err:=v2input.TaskID(workflow,"derivative.watermark",0);if err!=nil||predecessor!=v.producer.TaskID{return zero,nil,v2input.ErrInvalid}
 if _,err=delivery.DecodeWatermarkSourcePin(v.pin.JCS(),v.pin.SHA256(),v.scope,v.config);err!=nil{return zero,nil,err}
 config,err:=exportpreset.DecodeFrozenConfig(v.config,v.scope.PresetSHA);if err!=nil||config.Watermark==nil{return zero,nil,v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(v.output.ImageJSON);if err!=nil||renderDerivativeBytesSHA(v.output.ImageJSON)!=v.output.ImageRef.SHA256{return zero,nil,v2input.ErrInvalid}
 // These operations already ran; metadata is applied by metadata.write after
 // clean intermediate encoding. Frozen request/config bytes remain untouched.
 config.Size=nil;config.Sharpen=nil;config.Watermark=nil;config.MetadataPolicy="strip"
 raw,err:=canonical.Marshal(config);if err!=nil{return zero,nil,err}
 var preset DownloadPreset;if json.Unmarshal(raw,&preset)!=nil{return zero,nil,v2input.ErrInvalid}
 nodes,err:=CompileDownloadExports(DownloadExportSpec{RunID:request.ChildRunID,TargetICCSHA256:targetICC,Preset:preset,Items:[]DownloadExportItem{{Ordinal:0,Manifest:v.output.ImageRef,Image:image}}},registry)
 if err!=nil{return zero,nil,err};if len(nodes)!=1{return zero,nil,v2input.ErrInvalid}
 node:=nodes[0]
 node.ID,err=v2input.TaskID(workflow,"export.encode",0);if err!=nil{return zero,nil,err}
 node.Key,err=v2input.NodeKey("export.encode",0);if err!=nil{return zero,nil,err}
 profile,err:=CompileDeliveryEncodeProfile(node,v.output.ImageJSON,limits);if err!=nil{return zero,nil,err}
 return node,profile,nil
}
