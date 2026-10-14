package workflow

import (
 "context"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)

// StageClaimed loads all materialization parameters from the current lease.
// Work discovery/claim/release remains the surrounding scheduler loop's duty.
func(s DeliveryMetadataStage)StageClaimed(ctx context.Context,lease delivery.MetadataBarrierLease)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63{return v2input.ErrInvalid}
 inputs,err:=delivery.NewMetadataStageInputStore(s.DB,lease.Tenant);if err!=nil{return err}
 input,err:=inputs.Load(ctx,lease);if err!=nil{return err}
 config:=input.Config;request:=config.Request
 preset,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),request.Preset.PresetSHA)
 if err!=nil||preset.Watermark!=nil{return v2input.ErrInvalid}
 return s.StagePublished(ctx,lease,PublishedDeliveryMetadataSpec{ParentRun:request.ParentRunID,ChildRun:request.ChildRunID,
 SlotIndex:request.SlotIndex,Encoded:input.Encoded,EncodedJCS:input.EncodedJCS,EncodedBytes:input.EncodedBytes,
 Binding:request.Binding,Metadata:config.Metadata,Policy:preset.MetadataPolicy,ApprovedFields:config.Approved})
}
