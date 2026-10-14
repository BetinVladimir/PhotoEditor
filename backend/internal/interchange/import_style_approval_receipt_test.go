package interchange

import (
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "github.com/google/uuid"
)
func TestStyleApprovalReceiptRejectsMissingOrForeignEvidence(t *testing.T){
 for _,receipt:=range []ImportStyleApprovalReceipt{{},{Parsed:"{}",Config:"{}"},{Parsed:"null",Config:"{}"}}{
  if err:=validateImportStyleApprovalReceipt(uuid.New(),uuid.New(),receipt);err==nil{t.Fatal("accepted absent style approval evidence")}
 }
}

func TestStyleApprovalRecipeRequiresExactStyleAndSource(t *testing.T){
 scope:=preparedPublisherScope()
 style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))}
 raw,err:=canonical.Marshal(map[string]any{
  "schema_version":3,"source":scope.Binding,"base_style":style,
  "session_corrections":nil,"learning_version_id":nil,"overrides":[]v2input.Ref{},
  "layer_stack":nil,"renderer_version":"v2.2",
  "parameter_delta":map[string]float64{"ev":0,"log_r":0,"log_g":0,"log_b":0,"saturation_delta":0,"retouch_delta":0},
 })
 if err!=nil{t.Fatal(err)}
 receipt:=ImportStyleApprovalReceipt{Recipe:string(raw),Binding:scope.Binding,Style:style}
 if err:=validateImportStyleRecipe(receipt);err!=nil{t.Fatal("exact recipe rejected",err)}
 for _,change:=range []string{"style_id","style_sha","source","version","canvas","missing","malformed","oversize"}{
  changed:=receipt
  switch change{
  case "style_id":changed.Style.ArtifactID=uuid.New()
  case "style_sha":changed.Style.SHA256=rawSHA([]byte("other style"))
  case "source":changed.Binding.Source.ArtifactID=uuid.New()
  case "version":changed.Binding.AssetVersionID=uuid.New()
  case "canvas":changed.Binding.CanvasHash=rawSHA([]byte("other canvas"))
  case "missing":changed.Recipe=""
  case "malformed":changed.Recipe="{}"
  case "oversize":changed.Recipe=string(make([]byte,(8<<20)+1))
  }
  if err:=validateImportStyleRecipe(changed);err==nil{t.Fatal("accepted substituted recipe evidence",change)}
 }
}
