package workflow

import (
 "testing"
 "context"
 "encoding/binary"
 "encoding/json"
 "math"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestDeliveryWatermarkResultPinnedMetadataAndReports(t *testing.T){
 _,parent,child,sourceRaw,base:=deliveryExportPlanFixture(t,func(*exportpreset.Config){})
 image,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{t.Fatal(err)}
 mark:=image;mark.Canvas.Width=100;mark.Canvas.Height=50
 mark.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:downloadDigest("mark pixels")}
 alpha:=review.ArtifactRef{ID:uuid.New(),SHA256:downloadDigest("mark alpha")};mark.Alpha=&alpha
 markRaw,err:=canonical.Marshal(mark);if err!=nil{t.Fatal(err)}
 markRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(markRaw)};canvas,_:=canonical.Hash(mark.Canvas)
 asset,version:=uuid.New(),uuid.New()
 evidence:=delivery.WatermarkSourceEvidence{Source:delivery.WatermarkSource{Asset:asset,Version:version,Ref:markRef},Binding:v2input.SessionVersionBinding{AssetVersionID:version,Source:markRef,CanvasHash:canvas},Manifest:markRaw,Arrays:[]delivery.WatermarkArrayObject{
  {Contract:"WorkingPixels",Ref:v2input.Ref{ArtifactID:mark.Pixels.ID,SHA256:mark.Pixels.SHA256},Object:objectstore.Object{SHA256:mark.Pixels.SHA256,Bytes:60128}},
  {Contract:"WorkingAlpha",Ref:v2input.Ref{ArtifactID:alpha.ID,SHA256:alpha.SHA256},Object:objectstore.Object{SHA256:alpha.SHA256,Bytes:10128}},
 }}
 for _,opacity:=range []float64{0,.5}{
  config,err:=canonical.Marshal(map[string]any{"format":"jpeg","icc_id":"srgb_v1","quality":92,"subsampling":"444","alpha":"matte","matte_rgb":[]int{1,1,1},"metadata_policy":"strip","naming_template":"{original_name}_edited","watermark":map[string]any{"asset_id":asset,"opacity":opacity,"anchor":"center","margin_x":0,"margin_y":0}});if err!=nil{t.Fatal(err)}
  scope:=delivery.WatermarkPinScope{Tenant:uuid.New(),Project:uuid.New(),Actor:uuid.New(),Parent:parent,Preset:uuid.New(),PresetSHA:renderDerivativeBytesSHA(config)}
  pin,err:=delivery.CompileWatermarkSourcePin(scope,config,evidence);if err!=nil{t.Fatal(err)}
  sourceRef:=v2input.Ref{ArtifactID:child.Binding.Source.ArtifactID,SHA256:renderDerivativeBytesSHA(sourceRaw)}
  stage,err:=pin.CompileStage(scope,config,sourceRef,sourceRaw);if err!=nil{t.Fatal(err)}
  node:=CompiledDownloadNode{Operation:"derivative.watermark",Version:"1",ResourceClass:"cpu",Params:stage.Params}
  for _,ref:=range stage.Inputs{node.Inputs=append(node.Inputs,review.ArtifactRef{ID:ref.ArtifactID,SHA256:ref.SHA256})}
  for _,mode:=range []string{"valid","color","canvas","input_alias","report_source","report_operation","report_applied","report_delta","report_warning"}{t.Run(mode,func(t *testing.T){
   output:=image;output.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:downloadDigest("new pixels")}
   if image.Alpha!=nil{a:=review.ArtifactRef{ID:uuid.New(),SHA256:downloadDigest("new alpha")};output.Alpha=&a}
   report:=DeliveryDerivativeReport{Image:node.Inputs[0],Operation:"watermark",Applied:opacity>0,Width:image.Canvas.Width,Height:image.Canvas.Height,MaxChannelDelta:.1,Warnings:[]string{}}
   if opacity==0{report.MaxChannelDelta=0;report.Warnings=[]string{"zero_opacity"}}
   switch mode{case "color":output.Color.ICCSHA256=downloadDigest("foreign profile");case "canvas":output.Canvas.SpaceID="foreign";case "input_alias":output.Pixels=image.Pixels;case "report_source":report.Image=node.Inputs[len(node.Inputs)-3];case "report_operation":report.Operation="sharpen";case "report_applied":report.Applied=!report.Applied;case "report_delta":report.MaxChannelDelta=1.1;case "report_warning":report.Warnings=[]string{"zero_strength"}}
   raw,_:=canonical.Marshal(output);outRef:=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(raw)}
   reportRaw,_:=canonical.Marshal(report);reportRef:=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(reportRaw)}
   _,err:=ValidateDeliveryWatermarkResult(node,pin,scope,config,sourceRaw,outRef,raw,reportRef,reportRaw)
   if (err==nil)!=(mode=="valid"){t.Fatal(mode,opacity,err)}
   if mode=="valid"&&image.Alpha==nil{
    for _,physical:=range []string{"valid","corrupt","nonfinite","wrong_shape"}{
     rgb:=deliveryTestRGBNPY(image.Canvas.Width,image.Canvas.Height)
     if physical=="nonfinite"{binary.LittleEndian.PutUint32(rgb[len(rgb)-4:],0x7fc00000)}
     if physical=="wrong_shape"{rgb=deliveryTestRGBNPY(image.Canvas.Width+1,image.Canvas.Height)}
     current:=output;current.Pixels.SHA256=renderDerivativeBytesSHA(rgb)
     currentRaw,_:=canonical.Marshal(current)
     bodies:=deliveryStageObjects{"rgb":rgb,"image":currentRaw,"report":reportRaw}
     taskNode:=node;workflow,_:=v2input.WorkflowID(child.RunID);taskNode.ID,_=v2input.TaskID(workflow,"derivative.watermark",0)
     result:=DeliveryStageResult{TaskID:taskNode.ID,ResultID:uuid.New(),Attempt:1,Fence:1}
     for _,entry:=range []struct{key,role,media string;id uuid.UUID}{
      {"rgb","working_rgb","application/octet-stream",current.Pixels.ID},{"image","working_image","application/json",outRef.ID},{"report","derivative_report","application/json",reportRef.ID},
     }{body:=bodies[entry.key];result.Outputs=append(result.Outputs,DeliveryStageOutput{Ref:v2input.Ref{ArtifactID:entry.id,SHA256:renderDerivativeBytesSHA(body)},Role:entry.role,Bucket:"photos",Key:entry.key,Version:"pinned-v1",Bytes:int64(len(body)),MediaType:entry.media})}
     if physical=="corrupt"{changed:=append([]byte(nil),rgb...);changed[len(changed)-1]^=1;bodies["rgb"]=changed}
     _,err:=VerifyDeliveryWatermarkOutput(context.Background(),bodies,"photos",taskNode,pin,scope,config,sourceRaw,result)
     if (err==nil)!=(physical=="valid"){t.Fatal("physical verification",physical,opacity,err)}
     if physical=="valid"{
      registry,err:=LoadRegistry();if err!=nil{t.Fatal(err)}
      limits:=DeliveryDerivativeLimits{RAMBytes:math.MaxInt64,ScratchBytes:math.MaxInt64,OutputBytes:math.MaxInt64}
      request:=delivery.FrozenExportRequest{SchemaVersion:1,ParentRunID:parent,ChildRunID:child.RunID,SlotIndex:0,Binding:child.Binding,Recipe:child.Recipe,ConfigJCS:string(config),Preset:delivery.FrozenPreset{PresetID:scope.Preset,PresetSHA:scope.PresetSHA,Config:v2input.Ref{ArtifactID:uuid.New(),SHA256:scope.PresetSHA}}}
      exerciseWatermarkPrefix(t,request,scope,evidence,sourceRaw,registry,limits)
      lease:=delivery.ExpansionLease{Tenant:scope.Tenant,Run:parent,Expansion:uuid.New(),Token:uuid.New()}
      producerRaw,_:=json.Marshal(result)
      for _,recheck:=range []string{"stable","revoked_pin","changed_producer"}{
       pinReader:=&watermarkLoaderPins{pin:pin,refuseSecond:recheck=="revoked_pin"}
       final:=producerRaw;if recheck=="changed_producer"{changed:=result;changed.Fence++;final,_=json.Marshal(changed)}
       db:=&deliveryStageReadDB{responses:[][]byte{producerRaw,final}}
       verified,err:=LoadVerifiedDeliveryWatermarkStage(context.Background(),db,pinReader,bodies,"photos",lease,request,scope,sourceRef,sourceRaw,registry,limits)
       if (err==nil)!=(recheck=="stable"){t.Fatal("loader recheck",recheck,err)}
       if recheck=="stable"{if verified==nil||db.calls!=2||pinReader.calls!=2{t.Fatal("missing authority rechecks")};encoded,profile,err:=verified.CompileEncodeAfterWatermark(request,base.TargetICCSHA256,registry,limits);if err!=nil||len(profile)==0||encoded.Operation!="export.encode"||encoded.Inputs[0]!=verified.Output().ImageRef{t.Fatal("encode after watermark",err)};currentImage,_:=review.ParseWorkingImage(verified.Output().ImageJSON);inventory:=[]v2input.Entry{
 {Ref:sourceRef,Contract:"WorkingImage"},{Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Contract:"NpyArray"},
 {Ref:markRef,Contract:"WorkingImage"},{Ref:evidence.Arrays[0].Ref,Contract:"NpyArray"},{Ref:evidence.Arrays[1].Ref,Contract:"NpyArray"},
 {Ref:v2input.Ref{ArtifactID:verified.Output().ImageRef.ID,SHA256:verified.Output().ImageRef.SHA256},Contract:"WorkingImage"},{Ref:v2input.Ref{ArtifactID:currentImage.Pixels.ID,SHA256:currentImage.Pixels.SHA256},Contract:"NpyArray"},
 {Ref:v2input.Ref{ArtifactID:verified.Output().ReportRef.ID,SHA256:verified.Output().ReportRef.SHA256},Contract:"DerivativeReport"},{Ref:request.Recipe,Contract:"EditingRecipeV3"},{Ref:request.Preset.Config,Contract:"ExportPresetConfig"},
 };paramsRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(encoded.Params)};frozen,err:=verified.FreezeEncodeAfterWatermark(request,base.TargetICCSHA256,registry,limits,paramsRef,inventory);if err!=nil{t.Fatal("freeze watermark encoder",err)};draft,err:=frozen.DraftTask();if err!=nil||draft.ID!=encoded.ID||frozen.Predecessor()!=result.TaskID||frozen.Stage()!=1{t.Fatal("watermark encode identity",err)};if _,err:=verified.FreezeEncodeAfterWatermark(request,base.TargetICCSHA256,registry,limits,paramsRef,inventory[1:]);err==nil{t.Fatal("missing predecessor source accepted")};exerciseWatermarkEncodeCoordinator(t,frozen);createDB:=&deliveryStageRecordDB{accepted:true};if err:=frozen.CreateInTransaction(context.Background(),createDB,lease,request);err!=nil||createDB.query!=watermarkEncodeCreateQuery{t.Fatal("blocked watermark encoder",err)};if ready,err:=frozen.TransportRegistered(context.Background(),createDB,lease,request);err!=nil||!ready||createDB.query!=watermarkEncodeTransportQuery{t.Fatal("watermark transport receipt",err)};wrongTransport:=request;wrongTransport.SlotIndex++;transportCalls:=createDB.calls;if _,err:=frozen.TransportRegistered(context.Background(),createDB,lease,wrongTransport);err==nil||createDB.calls!=transportCalls{t.Fatal("foreign transport scope queried")};activationDB:=&watermarkActivationDB{answers:[]bool{false,true}};if err:=frozen.ActivateInTransaction(context.Background(),activationDB,lease,request);err!=nil||len(activationDB.queries)!=2||activationDB.queries[0]!=watermarkEncodeActivatedQuery||activationDB.queries[1]!=watermarkEncodeActivateQuery{t.Fatal("watermark first activation",err)};replayDB:=&watermarkActivationDB{answers:[]bool{true}};if err:=frozen.ActivateInTransaction(context.Background(),replayDB,lease,request);err!=nil||len(replayDB.queries)!=1{t.Fatal("watermark activation replay",err)};refusedDB:=&watermarkActivationDB{answers:[]bool{false,false}};if err:=frozen.ActivateInTransaction(context.Background(),refusedDB,lease,request);err==nil{t.Fatal("activation refusal ignored")};foreignActivationDB:=&watermarkActivationDB{};if err:=frozen.ActivateInTransaction(context.Background(),foreignActivationDB,lease,wrongTransport);err==nil||len(foreignActivationDB.queries)!=0{t.Fatal("foreign activation queried")};createDB.accepted=false;if ready,err:=frozen.TransportRegistered(context.Background(),createDB,lease,request);err!=nil||ready{t.Fatal("missing transport acknowledged",err)};if err:=frozen.CreateInTransaction(context.Background(),createDB,lease,request);err==nil{t.Fatal("task refusal ignored")};changed:=request;changed.ChildRunID=uuid.New();callsBefore:=createDB.calls;if err:=frozen.CreateInTransaction(context.Background(),createDB,lease,changed);err==nil||createDB.calls!=callsBefore{t.Fatal("foreign encoder scope queried")};var params map[string]json.RawMessage;if json.Unmarshal(encoded.Params,&params)!=nil||string(params["metadata_policy"])!="\"strip\""||len(params["watermark"])!=0||len(params["sharpen"])!=0{t.Fatal("repeated presentation or metadata")};foreignRequest:=request;foreignRequest.ChildRunID=uuid.New();if _,_,err:=verified.CompileEncodeAfterWatermark(foreignRequest,base.TargetICCSHA256,registry,limits);err==nil{t.Fatal("foreign child encode")};record:=&deliveryStageRecordDB{accepted:true};if err:=verified.RecordInTransaction(context.Background(),record,lease,request,"photos");err!=nil||record.query!=watermarkStageRecordQuery{t.Fatal("watermark receipt",err)};if err:=verified.BeginEncodeContinuationInTransaction(context.Background(),record,lease,request,"photos");err!=nil||record.query!=`SELECT photo.begin_v30_watermark_encode_continuation($1,$2,$3,$4,$5::jsonb,$6,$7)`{t.Fatal("watermark barrier",err)};record.accepted=false;if err:=verified.RecordInTransaction(context.Background(),record,lease,request,"photos");err==nil{t.Fatal("SQL receipt refusal ignored")};foreign:=lease;foreign.Run=uuid.New();before:=record.calls;if err:=verified.RecordInTransaction(context.Background(),record,foreign,request,"photos");err==nil||record.calls!=before{t.Fatal("foreign receipt scope queried")};copy:=verified.Output();copy.ImageJSON[0]='!';if verified.Output().ImageJSON[0]!='{'{t.Fatal("borrowed output")}}
      }
     }
    }
   }
  })}
 }
}
