package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
func TestExportFrozenValidatorUsesExistingDecoderAndPins(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("canvas"))};style:=ref()
 authored,err:=canonical.Marshal(map[string]any{"schema_version":3,"source":binding,"base_style":style,"session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},"layer_stack":nil,"renderer_version":"v2.2","parameter_delta":v2draft.CorrectionVector{}});if err!=nil{t.Fatal(err)}
 icc,model:=rawSHA([]byte("icc")),rawSHA([]byte("model"))
 raw,err:=v2draft.BuildFrozenRenderRecipe(authored,v2draft.ResolvedRecipeInputs{Binding:binding,BaseStyle:style,Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1},Saturation:1},ICCLockSHA:icc,ModelLockSHA:model},nil);if err!=nil{t.Fatal(err)}
 reference:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
 for _,mode:=range []string{"valid","binding","icc","model","hash"}{t.Run(mode,func(t *testing.T){
  receipt:=exportRecipeBinding{ref:reference,binding:binding};validator:=ExportFrozenDocumentValidator{icc:icc,model:model}
  body:=append([]byte(nil),raw...)
  switch mode{case "binding":receipt.binding.AssetVersionID=uuid.New();case "icc":validator.icc=rawSHA([]byte("other"));case "model":validator.model=rawSHA([]byte("other"));case "hash":body=append(body,' ')}
  validator.recipes=map[uuid.UUID]exportRecipeBinding{reference.ArtifactID:receipt}
  children,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:reference,Contract:"FrozenRenderRecipe"},body)
  if mode=="valid"{if err!=nil||len(children)!=2||children[0].Contract!="WorkingImage"||children[1].Contract!="StyleV2"{t.Fatal("frozen refs invalid",err)}}else if err==nil{t.Fatal("invalid frozen authority accepted")}
 })}
}
