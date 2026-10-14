package workflow

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
type DeliveryWatermarkPinReader interface{Read(context.Context,delivery.WatermarkPinScope,uuid.UUID,uuid.UUID,[]byte)(delivery.FrozenWatermarkSource,error)}
// VerifiedDeliveryWatermarkStage is process-local evidence only. SQL commit must
// recheck source pin, predecessor, producer/attempt/fence and output authority.
type VerifiedDeliveryWatermarkStage struct{sourceRef v2input.Ref;source []byte;pin delivery.FrozenWatermarkSource;scope delivery.WatermarkPinScope;config []byte;producer DeliveryStageResult;output DeliveryPresentationOutput}
func(v VerifiedDeliveryWatermarkStage)Producer()DeliveryStageResult{out:=v.producer;out.Outputs=append([]DeliveryStageOutput(nil),out.Outputs...);return out}
func(v VerifiedDeliveryWatermarkStage)Output()DeliveryPresentationOutput{out:=v.output;out.ImageJSON=append([]byte(nil),out.ImageJSON...);out.ReportJSON=append([]byte(nil),out.ReportJSON...);return out}

func LoadVerifiedDeliveryWatermarkStage(ctx context.Context,db artifactjson.RowQuerier,pins DeliveryWatermarkPinReader,objects v2input.SessionAnalysisObjectReader,bucket string,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,scope delivery.WatermarkPinScope,imageRef v2input.Ref,imageRaw []byte,registry Registry,limits DeliveryDerivativeLimits)(*VerifiedDeliveryWatermarkStage,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if db==nil||pins==nil||objects==nil||scope.Tenant!=lease.Tenant||scope.Parent!=lease.Run||request.ParentRunID!=scope.Parent||request.Preset.PresetID!=scope.Preset||request.Preset.PresetSHA!=scope.PresetSHA||request.Preset.Config.SHA256!=scope.PresetSHA{return nil,v2input.ErrInvalid}
 config:=[]byte(request.ConfigJCS);source:=append([]byte(nil),imageRaw...)
 pin,err:=pins.Read(ctx,scope,lease.Expansion,lease.Token,config);if err!=nil{return nil,err}
 // Independently recompile even when the configured reader returns typed values.
 pin,err=delivery.DecodeWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,config);if err!=nil{return nil,err}
 node,_,err:=CompileDeliveryWatermarkNode(request.ChildRunID,pin,scope,config,imageRef,source,registry,limits);if err!=nil{return nil,err}
 producer,err:=ReadWatermarkStageResult(ctx,db,lease,request,bucket);if err!=nil||producer==nil{return nil,err}
 expected,err:=canonical.Marshal(producer);if err!=nil{return nil,err}
 output,err:=VerifyDeliveryWatermarkOutput(ctx,objects,bucket,node,pin,scope,config,source,*producer);if err!=nil{return nil,err}
 freshPin,err:=pins.Read(ctx,scope,lease.Expansion,lease.Token,config);if err!=nil{return nil,err}
 if freshPin.SHA256()!=pin.SHA256()||!bytes.Equal(freshPin.JCS(),pin.JCS()){return nil,v2input.ErrInvalid}
 fresh,err:=ReadWatermarkStageResult(ctx,db,lease,request,bucket);if err!=nil{return nil,err};if fresh==nil{return nil,v2input.ErrInvalid}
 actual,err:=canonical.Marshal(fresh);if err!=nil||!bytes.Equal(expected,actual){return nil,v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return nil,err}
 return &VerifiedDeliveryWatermarkStage{sourceRef:imageRef,source:append([]byte(nil),source...),pin:pin,scope:scope,config:append([]byte(nil),config...),producer:*fresh,output:output},nil
}
