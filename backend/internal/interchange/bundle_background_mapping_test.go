package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestBundleBackgroundMappingExactStyleAndImageInventory(t *testing.T){
 style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))};image:=uuid.New();sha:=rawSHA([]byte("image"))
 inventory:=[]exportBundleInventoryEntry{{ID:style.ArtifactID,SHA:style.SHA256,Path:"dependencies/style.json",Contract:"StyleV2"},{ID:image,SHA:sha,Path:"dependencies/image.bin",Contract:"StyleBackgroundAsset"}}
 for _,mode:=range []string{"valid","style","path","sha","portable","duplicate"}{t.Run(mode,func(t *testing.T){
  binding:=bundleBackground{Style:style,Portable:"studio",Path:"dependencies/image.bin",SHA:sha}
  switch mode{case "style":binding.Style.SHA256=sha;case "path":binding.Path="foreign.bin";case "sha":binding.SHA=style.SHA256;case "portable":binding.Portable=" studio "}
  bindings:=[]bundleBackground{binding};if mode=="duplicate"{bindings=append(bindings,binding)}
  err:=validateBundleBackgroundMappings(bindings,inventory);if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected mapping: %v",err)}
 })}
}
