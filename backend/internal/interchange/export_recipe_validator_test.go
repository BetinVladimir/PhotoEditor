package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestExportRecipeValidatorPinsBindingAndChildRoles(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(uuid.NewString()))}}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:rawSHA([]byte("canvas"))}
 root:=map[string]any{"schema_version":3,"source":binding,"base_style":ref(),"session_corrections":ref(),"learning_version_id":nil,"overrides":[]v2input.Ref{ref()},"layer_stack":ref(),"protected_mask":ref(),"renderer_version":"v2.2","parameter_delta":map[string]any{"ev":0,"log_r":0,"log_g":0,"log_b":0,"saturation_delta":0,"retouch_delta":0}}
 for _,mode:=range []string{"valid","binding","learning","alias","hash"}{t.Run(mode,func(t *testing.T){
  owned:=map[string]any{};for key,value:=range root{owned[key]=value}
  if mode=="binding"{other:=binding;other.AssetVersionID=uuid.New();owned["source"]=other}
  if mode=="learning"{owned["learning_version_id"]=uuid.New()}
  if mode=="alias"{owned["base_style"]=binding.Source}
  raw,err:=canonical.Marshal(owned);if err!=nil{t.Fatal(err)}
  reference:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  validator:=ExportRecipeDocumentValidator{recipes:map[uuid.UUID]exportRecipeBinding{reference.ArtifactID:{ref:reference,binding:binding}}}
  if mode=="hash"{raw=append(raw,' ')}
  children,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Ref:reference,Contract:"EditingRecipeV3"},raw)
  imported:=ImportRecipeDocumentValidator{recipes:validator.recipes}
  importChildren,importErr:=imported.ImportDocumentChildren(context.Background(),v2input.Entry{Ref:reference,Contract:"EditingRecipeV3"},raw)
  if (importErr==nil)!=(err==nil)||len(importChildren)!=len(children){t.Fatal("import/export typed recipe decisions differ",importErr,err)}
  for i:=range children{if importChildren[i]!=children[i]{t.Fatal("import/export child roles differ")}}

  if mode=="valid"{if err!=nil||len(children)!=6||children[0].Contract!="WorkingImage"||children[1].Contract!="StyleV2"{t.Fatal("invalid recipe children",err)}}else if err==nil{t.Fatal("invalid recipe accepted")}
 })}
}
