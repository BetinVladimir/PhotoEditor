package v2input

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)
func TestSemanticAnalysisCompilersBindExactSourceParamsAndRuntime(t *testing.T){
 raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
 var images []json.RawMessage;if json.Unmarshal(raw,&images)!=nil||len(images)==0{t.Fatal("image fixture")}
 refFor:=func(body []byte)Ref{sum:=sha256.Sum256(body);return Ref{uuid.New(),hex.EncodeToString(sum[:])}}
 pins:=SemanticAnalysisRuntimePins{strings.Repeat("a",64),strings.Repeat("b",64),strings.Repeat("c",64)}
 for _,body:=range images{
  image,err:=review.ParseWorkingImage(body);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=SessionVersionBinding{AssetVersionID:uuid.New(),Source:refFor(body),CanvasHash:canvas}
  for _,operation:=range []string{"scene.parse","depth.predict"}{
   prepared,err:=PrepareSemanticAnalysisParams(context.Background(),operation,binding,body,pins.ModelLockSHA);if err!=nil{t.Fatal(err)}
   var params map[string]any;if json.Unmarshal(prepared.Params,&params)!=nil{t.Fatal("params")}
   if params["model_lock_id"]!=pins.ModelLockSHA{t.Fatal("lost model pin")}
   if operation=="scene.parse"&&params["preprocessing_id"]!="segformer_ade512_official_v1"{t.Fatal("scene preprocessing drift")}
   if operation=="depth.predict"&&params["input_size"]!=float64(518){t.Fatal("depth shape drift")}
   pref:=refFor(prepared.Params)
   sizes:=[]SemanticInputSize{{Ref:pref,Bytes:int64(len(prepared.Params))}}
   for _,entry:=range prepared.Inputs{count:=int64(1024);if entry.Ref==binding.Source{count=int64(len(body))};sizes=append(sizes,SemanticInputSize{Ref:entry.Ref,Bytes:count})}
   understated:=append([]SemanticInputSize(nil),sizes...);understated[0].Bytes--
   if _,err=CompileSemanticAnalysisTask(context.Background(),uuid.New(),0,operation,binding,body,pref,pins,understated,SemanticQualifiedCosts{VRAMBytes:1024},1<<40,1<<40,1<<40,1<<40);err==nil{t.Fatal("understated Params bytes accepted")}
   frozen,err:=CompileSemanticAnalysisTask(context.Background(),uuid.New(),0,operation,binding,body,pref,pins,sizes,SemanticQualifiedCosts{VRAMBytes:1024},1<<40,1<<40,1<<40,1<<40);if err!=nil{t.Fatal(err)}
   draft,err:=frozen.DraftTask();if err!=nil||draft.Operation!=operation||draft.ResourceClass!="gpu"{t.Fatalf("draft: %+v %v",draft,err)}
   inventory:=append(append([]Entry{},prepared.Inputs...),Entry{pref,"Params"})
   if err=frozen.VerifyInputInventory(context.Background(),inventory);err!=nil{t.Fatal(err)}
   metadata,err:=ResolveSemanticSourceMetadata(binding,body);if err!=nil{t.Fatal(err)}
   direct:=map[Ref]bool{pref:true,binding.Source:true,metadata.Inputs.Pixels:true}
   if len(frozen.closure.Direct)!=3{t.Fatal("native registration requires exactly three direct roots")}
   for i,ref:=range frozen.closure.Direct{if !direct[ref]{t.Fatal("optional alpha/geometry substituted for a direct root")};if i>0&&frozen.closure.Direct[i-1].ArtifactID.String()>=ref.ArtifactID.String(){t.Fatal("direct roots not canonical")}}
   if err=frozen.VerifyInputInventory(context.Background(),inventory[1:]);err==nil{t.Fatal("missing declared input accepted")}
   if _,err=CompileSemanticAnalysisTask(context.Background(),uuid.New(),0,operation,binding,body,pref,pins,sizes[:len(sizes)-1],SemanticQualifiedCosts{VRAMBytes:1024},1<<40,1<<40,1<<40,1<<40);err==nil{t.Fatal("missing exact input size accepted")}
  }
 }
}
func TestSemanticAnalysisProfilesChargeAlphaAndRejectBudgetUnderflow(t *testing.T){
 pins:=SemanticAnalysisRuntimePins{strings.Repeat("a",64),strings.Repeat("b",64),strings.Repeat("c",64)}
 var estimates []sessionDescribeEstimate
 for _,alpha:=range []bool{false,true}{
  raw,err:=CompileSemanticAnalysisProfile("depth.predict",100,80,alpha,1234,pins,SemanticQualifiedCosts{VRAMBytes:456},1<<40,1<<40,1<<40,1<<40);if err!=nil{t.Fatal(err)}
  var profile sessionDescribeExecutionProfile
  if json.Unmarshal(raw,&profile)!=nil{t.Fatal("profile")};estimates=append(estimates,profile.ResourceEstimate)
  b:=profile.ResourceEstimate
  for i:=0;i<4;i++{limits:=[]int64{b.RAMBytes,b.VRAMBytes,b.ScratchBytes,b.OutputBytes};limits[i]--;if _,err=CompileSemanticAnalysisProfile("depth.predict",100,80,alpha,1234,pins,SemanticQualifiedCosts{VRAMBytes:456},limits[0],limits[1],limits[2],limits[3]);err==nil{t.Fatal("underbudget accepted")}}
 }
 if estimates[1].RAMBytes<=estimates[0].RAMBytes||estimates[1].ScratchBytes<=estimates[0].ScratchBytes{t.Fatal("alpha buffers uncharged")}
}

func TestSemanticAnalysisCancellationPrecedesSourcePreparation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=PrepareSemanticAnalysisParams(ctx,"scene.parse",SessionVersionBinding{},nil,"");!errors.Is(err,context.Canceled){t.Fatalf("cancelled preparation: %v",err)}
 if _,err:=CompileSemanticAnalysisTask(ctx,uuid.Nil,0,"scene.parse",SessionVersionBinding{},nil,Ref{},SemanticAnalysisRuntimePins{},nil,SemanticQualifiedCosts{},0,0,0,0);!errors.Is(err,context.Canceled){t.Fatalf("cancelled compilation: %v",err)}
}

func TestSemanticSceneProfileReservesBoundedLogitsForSmallImages(t *testing.T){
 pins:=SemanticAnalysisRuntimePins{strings.Repeat("a",64),strings.Repeat("b",64),strings.Repeat("c",64)}
 raw,err:=CompileSemanticAnalysisProfile("scene.parse",8,8,false,1024,pins,SemanticQualifiedCosts{VRAMBytes:1},1<<40,1<<40,1<<40,1<<40);if err!=nil{t.Fatal(err)}
 var profile sessionDescribeExecutionProfile;if json.Unmarshal(raw,&profile)!=nil{t.Fatal("profile")}
 if profile.ResourceEstimate.RAMBytes<4<<30{t.Fatal("maximum legal logits were charged only by tiny source dimensions")}
}

func TestSemanticAnalysisCUDADeviceBudgetIsDecimalTenGB(t *testing.T){
 pins:=SemanticAnalysisRuntimePins{strings.Repeat("a",64),strings.Repeat("b",64),strings.Repeat("c",64)}
 for _,operation:=range []string{"scene.parse","depth.predict"}{
  if _,err:=CompileSemanticAnalysisProfile(operation,8,8,false,1024,pins,SemanticQualifiedCosts{VRAMBytes:10_000_000_000},1<<40,1<<40,1<<40,1<<40);err!=nil{t.Fatal(err)}
  if _,err:=CompileSemanticAnalysisProfile(operation,8,8,false,1024,pins,SemanticQualifiedCosts{VRAMBytes:10_000_000_001},1<<40,1<<40,1<<40,1<<40);err==nil{t.Fatal("over-budget CUDA prerequisite accepted")}
 }
}
