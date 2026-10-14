package workflow

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
const watermarkStageRecordQuery=`SELECT photo.record_v30_watermark_stage_verification($1,$2,$3,$4,$5::jsonb,$6,$7)`
// RecordInTransaction writes only a verification receipt. Caller owns the
// transaction; predecessor/adoption/continuation commit must be separate gates.
func(v VerifiedDeliveryWatermarkStage)RecordInTransaction(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,bucket string)error{return v.recordWatermark(ctx,tx,lease,request,bucket,false)}
// BeginEncodeContinuationInTransaction atomically records verification and opens
// a durable single-encode barrier. It does not adopt or dispatch either task.
func(v VerifiedDeliveryWatermarkStage)BeginEncodeContinuationInTransaction(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,bucket string)error{return v.recordWatermark(ctx,tx,lease,request,bucket,true)}
func(v VerifiedDeliveryWatermarkStage)recordWatermark(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,bucket string,begin bool)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||lease.Tenant==uuid.Nil||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||v.scope.Tenant!=lease.Tenant||v.scope.Parent!=lease.Run||request.ParentRunID!=lease.Run||request.SchemaVersion!=1||request.ChildRunID==uuid.Nil||request.Preset.PresetID!=v.scope.Preset||request.Preset.PresetSHA!=v.scope.PresetSHA||!bytes.Equal(v.config,[]byte(request.ConfigJCS)){return v2input.ErrInvalid}
 if _,err:=delivery.DecodeWatermarkSourcePin(v.pin.JCS(),v.pin.SHA256(),v.scope,v.config);err!=nil{return err}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return err}
 task,err:=v2input.TaskID(workflow,"derivative.watermark",0);if err!=nil{return err}
 producer,err:=canonical.Marshal(v.producer);if err!=nil{return err}
 if _,err=parseDeliveryStageResult(producer,task,bucket);err!=nil{return err}
 if len(v.output.ImageJSON)<2||len(v.output.ReportJSON)<2||renderDerivativeBytesSHA(v.output.ImageJSON)!=v.output.ImageRef.SHA256||renderDerivativeBytesSHA(v.output.ReportJSON)!=v.output.ReportRef.SHA256{return v2input.ErrInvalid}
 var accepted bool
 query:=watermarkStageRecordQuery;if begin{query=`SELECT photo.begin_v30_watermark_encode_continuation($1,$2,$3,$4,$5::jsonb,$6,$7)`}
 err=tx.QueryRow(ctx,query,lease.Expansion,lease.Token,request.SlotIndex,"derivative.watermark",string(producer),string(v.output.ImageJSON),string(v.output.ReportJSON)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_STAGE_VERIFICATION_REFUSED",409)};return ctx.Err()
}
