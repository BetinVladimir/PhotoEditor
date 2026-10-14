package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestResolvedChildrenRejectInventoryRelabelAndOmission(t *testing.T){
 image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}
 lut:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("lut"))}
 raw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"image":image},"absolute_lut":map[string]any{"artifact":lut}});if err!=nil{t.Fatal(err)}
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"}
 children,err:=exportResolvedChildren(entry,raw);if err!=nil||len(children)!=2{t.Fatal(children,err)}
 if err:=validateExportResolvedChildren(entry,raw,children);err!=nil{t.Fatal(err)}
 children[0].Contract="WorkingPixels"
 if err:=validateExportResolvedChildren(entry,raw,children);err==nil{t.Fatal("relabel accepted")}
 if err:=validateExportResolvedChildren(entry,raw,nil);err==nil{t.Fatal("omission accepted")}
}

func TestResolvedMaskPathsAreClosedAndIndicesBounded(t *testing.T){
 for _,mode:=range []string{"mask","warp","nested","extra-ref","index"}{t.Run(mode,func(t *testing.T){
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))}
  mask:=map[string]any{"artifact":ref}
  if mode=="warp"{mask=map[string]any{"transform_chain":map[string]any{"steps":[]any{map[string]any{"inverse_field":ref}}}}}
  if mode=="nested"{mask=map[string]any{"transform_chain":map[string]any{"steps":[]any{map[string]any{"nested":map[string]any{"inverse_field":ref}}}}}}
  if mode=="extra-ref"{mask=map[string]any{"invented":ref}}
  bindings:=[]any{map[string]any{"mask":mask}}
  if mode=="index"{bindings=make([]any,129);bindings[128]=map[string]any{"mask":mask}}
  raw,err:=canonical.Marshal(map[string]any{"params":map[string]any{"override":map[string]any{"mask_refs":bindings}}});if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"ResolvedStyle"}
  children,err:=exportResolvedChildren(entry,raw)
  if mode=="mask"||mode=="warp"{expected:="MaskPixels";if mode=="warp"{expected="DenseWarpField"};if err!=nil||len(children)!=1||children[0].Contract!=expected{t.Fatal(children,err)}}else if err==nil{t.Fatal("unknown/bounded path accepted")}
 })}
}
