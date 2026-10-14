package workflow

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)

// VerifiedWatermarkPresentationStage is local physical-verification evidence.
// SQL still repeats producer/fence and current source checks at commit.
type VerifiedWatermarkPresentationStage struct{
 stage VerifiedDeliveryStage
 scope delivery.WatermarkPinScope
 request []byte
 operation string
}
func(v VerifiedWatermarkPresentationStage)Producer()DeliveryStageResult{return v.stage.Producer()}
func(v VerifiedWatermarkPresentationStage)Output()DeliveryPresentationOutput{return v.stage.Output()}

func readWatermarkPresentationProducer(ctx context.Context,db artifactjson.RowQuerier,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,operation,bucket string)(*DeliveryStageResult,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if db==nil||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run!=request.ParentRunID||bucket==""||operation!="derivative.resize"&&operation!="derivative.sharpen"{return nil,v2input.ErrInvalid}
 var raw []byte
 if err:=db.QueryRow(ctx,`SELECT photo.v30_delivery_stage_result($1,$2,$3,$4)`,lease.Expansion,lease.Token,request.SlotIndex,operation).Scan(&raw);err!=nil{return nil,err}
 if len(raw)==0||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return nil,nil}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return nil,err}
 task,err:=v2input.TaskID(workflow,operation,0);if err!=nil{return nil,err}
 return parseDeliveryStageResult(raw,task,bucket)
}

// LoadVerifiedWatermarkPresentationStage validates the expected next derivative,
// verifies physical RGB/alpha outside SQL, then rereads producer and source pin.
func LoadVerifiedWatermarkPresentationStage(ctx context.Context,db artifactjson.RowQuerier,pins DeliveryWatermarkPinReader,objects v2input.SessionAnalysisObjectReader,bucket string,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,scope delivery.WatermarkPinScope,source []byte,prefix []DeliveryPresentationOutput,registry Registry,limits DeliveryDerivativeLimits)(*VerifiedWatermarkPresentationStage,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if db==nil||pins==nil||objects==nil||lease.Tenant!=scope.Tenant||lease.Run!=scope.Parent{return nil,v2input.ErrInvalid}
 config:=[]byte(request.ConfigJCS)
 pin,err:=pins.Read(ctx,scope,lease.Expansion,lease.Token,config);if err!=nil{return nil,err}
 next,err:=CompileDeliveryWatermarkPrefix(request,pin,scope,source,prefix,registry,limits);if err!=nil{return nil,err}
 if next.Node.Operation!="derivative.resize"&&next.Node.Operation!="derivative.sharpen"{return nil,v2input.ErrInvalid}
 producer,err:=readWatermarkPresentationProducer(ctx,db,lease,request,next.Node.Operation,bucket);if err!=nil||producer==nil{return nil,err}
 expected,err:=canonical.Marshal(producer);if err!=nil{return nil,err}
 output,err:=VerifyDeliveryStageOutput(ctx,objects,bucket,next.Node,next.SourceJCS,*producer);if err!=nil{return nil,err}
 freshPin,err:=pins.Read(ctx,scope,lease.Expansion,lease.Token,config);if err!=nil{return nil,err}
 if freshPin.SHA256()!=pin.SHA256()||!bytes.Equal(freshPin.JCS(),pin.JCS()){return nil,v2input.ErrInvalid}
 fresh,err:=readWatermarkPresentationProducer(ctx,db,lease,request,next.Node.Operation,bucket);if err!=nil{return nil,err};if fresh==nil{return nil,v2input.ErrInvalid}
 actual,err:=canonical.Marshal(fresh);if err!=nil||!bytes.Equal(actual,expected){return nil,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 return &VerifiedWatermarkPresentationStage{stage:VerifiedDeliveryStage{producer:*fresh,output:output},scope:scope,request:raw,operation:next.Node.Operation},nil
}

// BeginWatermarkInTransaction records verification and opens the watermark
// barrier only for the last required derivative. Caller owns rollback/commit.
func(v VerifiedWatermarkPresentationStage)BeginWatermarkInTransaction(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,bucket string)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||lease.Tenant!=v.scope.Tenant||lease.Run!=v.scope.Parent||request.ParentRunID!=lease.Run{return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil||!bytes.Equal(raw,v.request){return v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),v.scope.PresetSHA);if err!=nil{return err}
 previous:="derivative.resize";if config.Sharpen!=nil{previous="derivative.sharpen"}
 if v.operation!=previous{return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return err}
 task,err:=v2input.TaskID(workflow,previous,0);if err!=nil{return err}
 producer,err:=canonical.Marshal(v.stage.producer);if err!=nil{return err}
 if _,err=parseDeliveryStageResult(producer,task,bucket);err!=nil{return err}
 if len(v.stage.output.ImageJSON)<2||len(v.stage.output.ReportJSON)<2{return v2input.ErrInvalid}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.begin_v30_watermark_presentation_continuation($1,$2,$3,$4,$5::jsonb,$6,$7)`,lease.Expansion,lease.Token,request.SlotIndex,previous,string(producer),string(v.stage.output.ImageJSON),string(v.stage.output.ReportJSON)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_WATERMARK_PRESENTATION_BARRIER_REFUSED",409)}
 return nil
}

// BeginSharpenInTransaction records verified resize and opens the two-stage
// presentation barrier; it cannot skip sharpen or dispatch watermark.
func(v VerifiedWatermarkPresentationStage)BeginSharpenInTransaction(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,bucket string)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||lease.Tenant!=v.scope.Tenant||lease.Run!=v.scope.Parent||request.ParentRunID!=lease.Run{return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil||!bytes.Equal(raw,v.request){return v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),v.scope.PresetSHA);if err!=nil{return err}
 if config.Size==nil||config.Sharpen==nil{return v2input.ErrInvalid}
 previous:="derivative.resize"
 if v.operation!=previous{return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return err}
 task,err:=v2input.TaskID(workflow,previous,0);if err!=nil{return err}
 producer,err:=canonical.Marshal(v.stage.producer);if err!=nil{return err}
 if _,err=parseDeliveryStageResult(producer,task,bucket);err!=nil{return err}
 if len(v.stage.output.ImageJSON)<2||len(v.stage.output.ReportJSON)<2{return v2input.ErrInvalid}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.begin_v30_watermark_sharpen_continuation($1,$2,$3,$4,$5::jsonb,$6,$7)`,lease.Expansion,lease.Token,request.SlotIndex,previous,string(producer),string(v.stage.output.ImageJSON),string(v.stage.output.ReportJSON)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_WATERMARK_SHARPEN_BARRIER_REFUSED",409)}
 return nil
}
