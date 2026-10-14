package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestExportMaskSemanticsCanvasModelsAndBinaryEdge(t *testing.T){
 for _,mode:=range []string{"manual","model","foreign_canvas","missing_model","unknown_model","child","hash","cancelled","authority","missing_authority"}{t.Run(mode,func(t *testing.T){
  _,_,working:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(working);if err!=nil{t.Fatal(err)}
  hash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
  chain,err:=canonical.Marshal(map[string]any{"steps":[]any{},"hash":hash});if err!=nil{t.Fatal(err)}
  array:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:array.ArtifactID,SHA256:array.SHA256},Canvas:image.Canvas,TransformChain:chain,Selector:"manual",Confidence:1,Protected:true}
  lock:=rawSHA([]byte("installed model"));if mode=="model"||mode=="unknown_model"{mask.Selector="person";mask.ModelLockID=&lock};if mode=="missing_model"{mask.Selector="person"}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  semantics:=&ExportCanonicalMaskSemantics{canvases:map[string]bool{canvas:true},locks:map[string]bool{lock:true},fallback:&exportSemanticsStub{deny:mode=="authority"}}
  if mode=="missing_authority"{semantics.fallback=nil}
  if mode=="foreign_canvas"{semantics.canvases=nil};if mode=="unknown_model"{semantics.locks=nil}
  raw,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"MaskRef"}
  hints:=[]v2draft.LayerRef{{Ref:array,Contract:"MaskPixels"}}
  ctx:=context.Background();switch mode{case "child":hints[0].Contract="WorkingPixels";case "hash":raw=append(raw,' ');case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  err=semantics.QualifyExportDependency(ctx,entry,raw,hints)
  if (err==nil)!=(mode=="manual"||mode=="model"){t.Fatalf("unexpected mask qualification: %v",err)}
 })}
}

func TestExportMaskSemanticsConstructorOwnsLocksAndFallback(t *testing.T){
 _,_,intent,frozen,_:=exportExecutionFixture(t);fallback:=&exportSemanticsStub{}
 lock:=rawSHA([]byte("model"));locks:=[]string{lock}
 semantics,err:=NewExportCanonicalMaskSemantics(context.Background(),intent,frozen,locks,fallback);if err!=nil{t.Fatal(err)}
 locks[0]="changed";if !semantics.locks[lock]||semantics.locks[locks[0]]{t.Fatal("caller mutated installed model policy")}
 if err:=semantics.QualifyExportDependency(context.Background(),v2input.Entry{Contract:"LayerStack"},[]byte("{}"),nil);err!=nil||fallback.calls!=1{t.Fatal("fallback not invoked",err)}
 for _,bad:=range [][]string{{lock,lock},{"invalid"}}{if _,err:=NewExportCanonicalMaskSemantics(context.Background(),intent,frozen,bad,fallback);err==nil{t.Fatal("invalid locks accepted")}}
 if _,err:=NewExportCanonicalMaskSemantics(context.Background(),intent,frozen,nil,nil);err==nil{t.Fatal("missing fallback accepted")}
}

func TestExportManualMaskSemanticsBindsCanvasAndRequiresIndependentProvenance(t *testing.T){
 for _,mode:=range []string{"valid","foreign_canvas","source","point","sequence","child","authority","hash","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,rawWorking:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(rawWorking);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  point:=map[string]any{"x":0.25,"y":0.5}
  shape:=map[string]any{"kind":"stroke","sequence":0,"mode":"add","points":[]any{point},"radius":0.1,"hardness":0.8}
  document:=map[string]any{"version":"manual_mask_v1","source_sha256":image.Canvas.SourceSHA256,"canvas":image.Canvas,"shapes":[]any{shape}}
  switch mode{
  case "source":document["source_sha256"]=rawSHA([]byte("foreign original"))
  case "point":point["x"]=1.5
  case "sequence":delete(shape,"sequence")
  }
  raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ManualMask"}
  fallback:=&exportSemanticsStub{deny:mode=="authority"}
  semantics:=&ExportCanonicalMaskSemantics{canvases:map[string]bool{canvas:true},fallback:fallback}
  hints:=[]v2draft.LayerRef{}
  ctx:=context.Background()
  switch mode{
  case "foreign_canvas":semantics.canvases=nil
  case "child":hints=append(hints,v2draft.LayerRef{Ref:entry.Ref,Contract:"MaskPixels"})
  case "hash":raw=append(raw,' ')
  case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()
  }
  err=semantics.QualifyExportDependency(ctx,entry,raw,hints)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected vector qualification: %v",err)}
  expected:=0;if mode=="valid"||mode=="authority"{expected=1}
  if fallback.calls!=expected{t.Fatal("invalid geometry reached provenance adapter")}
 })}
}
