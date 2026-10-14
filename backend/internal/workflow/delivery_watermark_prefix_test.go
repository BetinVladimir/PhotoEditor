package workflow
import (
 "testing"
 "context"
 "photoeditor/backend/internal/review"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)
func exerciseWatermarkPrefix(t *testing.T,request delivery.FrozenExportRequest,scope delivery.WatermarkPinScope,evidence delivery.WatermarkSourceEvidence,source []byte,registry Registry,limits DeliveryDerivativeLimits){
 t.Helper()
 for _,mode:=range []string{"watermark","resize","sharpen","resize_sharpen"}{
  config,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),scope.PresetSHA);if err!=nil{t.Fatal(err)}
  if mode=="resize"||mode=="resize_sharpen"{edge:=200;config.Size=&exportpreset.Size{LongEdge:&edge,Fit:"contain"}}
  if mode=="sharpen"||mode=="resize_sharpen"{config.Sharpen=&exportpreset.Sharpen{Radius:1.2,Strength:.4,Threshold:.02}}
  raw,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
  changedScope:=scope;changedScope.PresetSHA=renderDerivativeBytesSHA(raw)
  changed:=request;changed.ConfigJCS=string(raw);changed.Preset.PresetSHA=changedScope.PresetSHA;changed.Preset.Config.SHA256=changedScope.PresetSHA
  pin,err:=delivery.CompileWatermarkSourcePin(changedScope,raw,evidence);if err!=nil{t.Fatal(err)}
  next,err:=CompileDeliveryWatermarkPrefix(changed,pin,changedScope,source,nil,registry,limits);if err!=nil{t.Fatal(mode,err)}
  expected:="derivative.watermark";if mode=="resize"||mode=="resize_sharpen"{expected="derivative.resize"}else if mode=="sharpen"{expected="derivative.sharpen"}
  if next.Node.Operation!=expected||next.Predecessor!=uuid.Nil||next.Stage!=0{t.Fatal(mode,"skipped prefix",next.Node.Operation)}
  image,err:=review.ParseWorkingImage(source);if err!=nil{t.Fatal(err)}
  inventory:=[]v2input.Entry{{Ref:changed.Binding.Source,Contract:"WorkingImage"},{Ref:changed.Recipe,Contract:"EditingRecipeV3"},{Ref:changed.Preset.Config,Contract:"ExportPresetConfig"},{Ref:v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256},Contract:"NpyArray"}}
  if image.Alpha!=nil{inventory=append(inventory,v2input.Entry{Ref:v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256},Contract:"NpyArray"})}
  if mode=="watermark"{
   stage,err:=pin.CompileStage(changedScope,raw,changed.Binding.Source,source);if err!=nil{t.Fatal(err)}
   markOffset:=2;if stage.ImageHasAlpha{markOffset=3}
   for n,ref:=range stage.Inputs{contract:="NpyArray";if n==0||n==markOffset{contract="WorkingImage"};inventory=append(inventory,v2input.Entry{Ref:ref,Contract:contract})}
  }
  params:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(next.Node.Params)}
  frozen,err:=FreezeDeliveryWatermarkPrefixTask(changed,pin,changedScope,source,nil,registry,limits,params,inventory);if err!=nil{t.Fatal(mode,"freeze prefix",err)}
  draft,err:=frozen.DraftTask();if err!=nil||draft.ID!=next.Node.ID||draft.Operation!=expected||frozen.Stage()!=0||frozen.Predecessor()!=uuid.Nil{t.Fatal(mode,"frozen prefix",err)}
  incomplete:=append([]v2input.Entry(nil),inventory...)
  for n,entry:=range incomplete{if entry.Contract=="EditingRecipeV3"{incomplete=append(incomplete[:n],incomplete[n+1:]...);break}}
  if _,err:=FreezeDeliveryWatermarkPrefixTask(changed,pin,changedScope,source,nil,registry,limits,params,incomplete);err==nil{t.Fatal("missing recipe accepted")}
  if mode!="watermark"{
   var outputs []DeliveryPresentationOutput
   current:=source
   for next.Node.Operation!="derivative.watermark"{
    if len(outputs)>=2{t.Fatal("presentation loop")}
    output,err:=review.ParseWorkingImage(current);if err!=nil{t.Fatal(err)}
    if next.Node.Operation=="derivative.resize"{
     canvas,chain,err:=deliveryResizeGeometry(next.Node,output);if err!=nil{t.Fatal(err)}
     output.Canvas=canvas;output.TransformChain=chain
    }
    output.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(deliveryTestRGBNPY(output.Canvas.Width,output.Canvas.Height))}
    if output.Alpha!=nil{alpha:=review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(watermarkPresentationAlphaNPY(output.Canvas.Width,output.Canvas.Height))};output.Alpha=&alpha}
    body,err:=canonical.Marshal(output);if err!=nil{t.Fatal(err)}
    operation:="resize";delta:=float64(0);if next.Node.Operation=="derivative.sharpen"{operation="sharpen";delta=.02}
    report,err:=canonical.Marshal(DeliveryDerivativeReport{Image:next.Node.Inputs[0],Operation:operation,Applied:true,Width:output.Canvas.Width,Height:output.Canvas.Height,MaxChannelDelta:delta,Warnings:[]string{}});if err!=nil{t.Fatal(err)}
    result:=DeliveryPresentationOutput{ImageRef:review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(body)},ImageJSON:body,ReportRef:review.ArtifactRef{ID:uuid.New(),SHA256:renderDerivativeBytesSHA(report)},ReportJSON:report}
    exerciseWatermarkPresentationLoader(t,changed,pin,changedScope,source,outputs,result,registry,limits)
    outputs=append(outputs,result)
    inventory=append(inventory,v2input.Entry{Ref:v2input.Ref{ArtifactID:result.ImageRef.ID,SHA256:result.ImageRef.SHA256},Contract:"WorkingImage"},v2input.Entry{Ref:v2input.Ref{ArtifactID:output.Pixels.ID,SHA256:output.Pixels.SHA256},Contract:"NpyArray"},v2input.Entry{Ref:v2input.Ref{ArtifactID:result.ReportRef.ID,SHA256:result.ReportRef.SHA256},Contract:"DerivativeReport"})
    if output.Alpha!=nil{inventory=append(inventory,v2input.Entry{Ref:v2input.Ref{ArtifactID:output.Alpha.ID,SHA256:output.Alpha.SHA256},Contract:"NpyArray"})}
    current=body
    next,err=CompileDeliveryWatermarkPrefix(changed,pin,changedScope,source,outputs,registry,limits);if err!=nil{t.Fatal(mode,"completed prefix",err)}
    if next.Node.Operation=="derivative.sharpen"{
     sharpenParams:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(next.Node.Params)}
     sharpenTask,err:=FreezeDeliveryWatermarkPrefixTask(changed,pin,changedScope,source,outputs,registry,limits,sharpenParams,inventory);if err!=nil{t.Fatal("freeze intermediate sharpen",err)}
     exerciseWatermarkSharpenCoordinator(t,sharpenTask)
     lease:=delivery.ExpansionLease{Tenant:changedScope.Tenant,Run:changed.ParentRunID,Expansion:uuid.New(),Token:uuid.New()}
     db:=&deliveryStageRecordDB{accepted:true}
     if err:=sharpenTask.CreateSharpenInTransaction(context.Background(),db,lease,changed);err!=nil||db.query!=watermarkSharpenCreateQuery{t.Fatal("sharpen draft ABI",err)}
     if len(db.args)!=9||db.args[8]!=string(current){t.Fatal("sharpen resource source must be actual resize image",db.args)}
     if ready,err:=sharpenTask.SharpenTransportRegistered(context.Background(),db,lease,changed);err!=nil||!ready||db.query!=watermarkSharpenTransportQuery{t.Fatal("sharpen transport ABI",err)}
     activation:=&watermarkActivationDB{answers:[]bool{false,true}}
     if err:=sharpenTask.ActivateSharpenInTransaction(context.Background(),activation,lease,changed);err!=nil||len(activation.queries)!=2||activation.queries[0]!=watermarkSharpenActivatedQuery||activation.queries[1]!=watermarkSharpenActivateQuery{t.Fatal("sharpen activation ABI",err)}
     replay:=&watermarkActivationDB{answers:[]bool{true}}
     if err:=sharpenTask.ActivateSharpenInTransaction(context.Background(),replay,lease,changed);err!=nil||len(replay.queries)!=1{t.Fatal("sharpen activation replay",err)}
    }

   }
   if next.Stage!=len(outputs)||next.Predecessor==uuid.Nil{t.Fatal("missing completed prefix identity")}
   last:=outputs[len(outputs)-1]
   stage,err:=pin.CompileStage(changedScope,raw,v2input.Ref{ArtifactID:last.ImageRef.ID,SHA256:last.ImageRef.SHA256},current);if err!=nil{t.Fatal(err)}
   markOffset:=2;if stage.ImageHasAlpha{markOffset=3}
   for n,ref:=range stage.Inputs{contract:="NpyArray";if n==0||n==markOffset{contract="WorkingImage"};inventory=append(inventory,v2input.Entry{Ref:ref,Contract:contract})}
   params=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(next.Node.Params)}
   completed,err:=FreezeDeliveryWatermarkPrefixTask(changed,pin,changedScope,source,outputs,registry,limits,params,inventory);if err!=nil{t.Fatal(mode,"freeze completed prefix",err)}
   lease:=delivery.ExpansionLease{Tenant:changedScope.Tenant,Run:changed.ParentRunID,Expansion:uuid.New(),Token:uuid.New()}
   createDB:=&deliveryStageRecordDB{accepted:true}
   if err:=completed.CreateInTransaction(context.Background(),createDB,lease,changed);err!=nil||createDB.query!=watermarkPresentationCreateQuery{t.Fatal("presentation draft ABI",err)}
   if ready,err:=completed.TransportRegistered(context.Background(),createDB,lease,changed);err!=nil||!ready||createDB.query!=watermarkPresentationTransportQuery{t.Fatal("presentation transport ABI",err)}
   activationDB:=&watermarkActivationDB{answers:[]bool{false,true}}
   if err:=completed.ActivateInTransaction(context.Background(),activationDB,lease,changed);err!=nil||len(activationDB.queries)!=2||activationDB.queries[0]!=watermarkPresentationActivatedQuery||activationDB.queries[1]!=watermarkPresentationActivateQuery{t.Fatal("presentation activation ABI",err)}
   replayDB:=&watermarkActivationDB{answers:[]bool{true}}
   if err:=completed.ActivateInTransaction(context.Background(),replayDB,lease,changed);err!=nil||len(replayDB.queries)!=1{t.Fatal("presentation activation replay",err)}
   createDB.accepted=false
   if err:=completed.CreateInTransaction(context.Background(),createDB,lease,changed);err==nil{t.Fatal("presentation draft refusal ignored")}
   refusedDB:=&watermarkActivationDB{answers:[]bool{false,false}}
   if err:=completed.ActivateInTransaction(context.Background(),refusedDB,lease,changed);err==nil{t.Fatal("presentation activation refusal ignored")}
   foreignRequest:=changed;foreignRequest.SlotIndex++
   before:=createDB.calls
   if err:=completed.CreateInTransaction(context.Background(),createDB,lease,foreignRequest);err==nil||createDB.calls!=before{t.Fatal("foreign presentation request reached SQL")}
   exerciseWatermarkPresentationCoordinator(t,completed)
   completedDraft,err:=completed.DraftTask();if err!=nil||completedDraft.Operation!="derivative.watermark"||completed.Predecessor()!=next.Predecessor||completed.Stage()!=len(outputs){t.Fatal("completed watermark descriptor",err)}
   if len(outputs)==2{if _,err:=CompileDeliveryWatermarkPrefix(changed,pin,changedScope,source,[]DeliveryPresentationOutput{outputs[1],outputs[0]},registry,limits);err==nil{t.Fatal("reordered prefix accepted")}}
   corrupt:=append([]DeliveryPresentationOutput(nil),outputs...);corrupt[len(corrupt)-1].ReportJSON=append(append([]byte(nil),last.ReportJSON...),' ')
   if _,err:=FreezeDeliveryWatermarkPrefixTask(changed,pin,changedScope,source,corrupt,registry,limits,params,inventory);err==nil{t.Fatal("substituted report accepted")}
  }
  original:=next.SourceJCS[0];next.SourceJCS[0]='!';if source[0]!=original{t.Fatal("borrowed source")}
  foreign:=changed;foreign.ChildRunID=uuid.New();if _,err:=CompileDeliveryWatermarkPrefix(foreign,pin,changedScope,source,nil,registry,limits);err==nil{t.Fatal("foreign child")}
  substituted:=append([]byte(nil),source...);substituted[len(substituted)-1]^=1;if _,err:=CompileDeliveryWatermarkPrefix(changed,pin,changedScope,substituted,nil,registry,limits);err==nil{t.Fatal("source substitution")}
  if _,err:=CompileDeliveryWatermarkPrefix(changed,pin,changedScope,source,[]DeliveryPresentationOutput{{}},registry,limits);err==nil{t.Fatal("unverified or extra result")}
  bad:=changed;bad.Preset.Config=v2input.Ref{ArtifactID:uuid.New(),SHA256:downloadDigest("foreign preset")};if _,err:=CompileDeliveryWatermarkPrefix(bad,pin,changedScope,source,nil,registry,limits);err==nil{t.Fatal("preset substitution")}
 }
}
