package interchange
import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type maskSemanticTargets struct{calls int;failure error}
func(s *maskSemanticTargets)QualifyImportBackgroundTarget(context.Context,v2input.SessionVersionBinding)error{s.calls++;return s.failure}
func TestBoundImportMaskSemanticsRechecksSourceAfterPolicy(t *testing.T){
 for _,mode:=range []string{"valid","canvas","chain","master_changed","policy","policy_revoked","source","cancel","missing"}{t.Run(mode,func(t *testing.T){
  _,entry,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:entry.Ref,CanvasHash:canvas}
  maskRaw,err:=canonical.Marshal(map[string]any{"artifact":v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))},"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"selector":"manual","confidence":1,"protected":true});if err!=nil{t.Fatal(err)}
  mask,err:=review.ParseMaskRef(maskRaw);if err!=nil{t.Fatal(err)}
  master:=&recipeSourceMasterReader{body:raw};targets:=&maskSemanticTargets{}
  sentinel:=errors.New("policy revoked");policyCalls:=0
  policy:=preparedInstallationMaskQualifier(func(_ context.Context,m review.MaskRef,_ v2input.SessionVersionBinding)error{policyCalls++;if string(m.TransformChain)!=string(image.TransformChain){t.Fatal("policy input mutated between rounds")};if len(m.TransformChain)>0{m.TransformChain[0]='!'};if mode=="policy_revoked"&&policyCalls==2{return sentinel};if mode=="policy"{return sentinel};return nil})
  q:=BoundImportMaskSemantics{Masters:master,Targets:targets,Policy:policy}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "canvas":binding.CanvasHash=rawSHA([]byte("foreign"));case "chain":mask.TransformChain=json.RawMessage(`{"steps":[]}`);case "master_changed":master.second=[]byte(`{}`);case "source":targets.failure=sentinel;case "cancel":cancel();case "missing":q.Policy=nil}
  err=q.QualifyImportedMask(ctx,mask,binding)
  if mode=="valid"{if err!=nil||master.calls!=3||targets.calls!=3||policyCalls!=2{t.Fatal("source recheck",err)};if string(mask.TransformChain)!=string(image.TransformChain){t.Fatal("policy mutated caller mask")}}else if err==nil{t.Fatal("unqualified semantics accepted")}
  if (mode=="source"||mode=="policy"||mode=="policy_revoked")&&!errors.Is(err,sentinel){t.Fatal("policy/source refusal reclassified",err)}
  if mode=="cancel"&&!errors.Is(err,context.Canceled){t.Fatal(err)}
  if (mode=="canvas"||mode=="chain"||mode=="source"||mode=="cancel"||mode=="missing")&&policyCalls!=0{t.Fatal("invalid source reached policy")}
 })}
}
