package interchange
import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type expandingImportCodec struct{child v2input.Ref;prepared []byte;calls,published int}
func(c *expandingImportCodec)RemapImportDocument(_ context.Context,_ string,raw []byte,_,_ v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 c.calls++
 if string(raw)==`{"parent":true}`{for i:=0;i<2;i++{if _,err:=resolve(c.child,"Override");err!=nil{return nil,err}}}
 return append([]byte(nil),c.prepared...),nil
}
func(c *expandingImportCodec)QualifyRemappedImportDocument(context.Context,string,[]byte,v2input.SessionVersionBinding)error{return nil}
func(c *expandingImportCodec)PublishImportDependency(_ context.Context,_ uuid.UUID,_ string,ref v2input.Ref,_ []byte)(v2input.Ref,error){c.published++;return ref,nil}
func TestRecursiveImportPreparedBudgetBoundsExpansionAndCountsSharedChildOnce(t *testing.T){
 for _,mode:=range []string{"exact","overflow","public"}{t.Run(mode,func(t *testing.T){
  parentRaw,childRaw:=[]byte(`{"parent":true}`),[]byte(`{"child":true}`)
  parent:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(parentRaw)};child:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(childRaw)}
  parentStaged,childStaged:=uuid.New(),uuid.New();parsed:=importParsedValidatorFixture()
  for _,item:=range []struct{ref v2input.Ref;staged uuid.UUID;path string}{{parent,parentStaged,"documents/parent.json"},{child,childStaged,"documents/child.json"}}{
   parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:item.ref.ArtifactID,Path:item.path,SHA:item.ref.SHA256,Contract:"Override"})
   parsed.Files=append(parsed.Files,importParsedFile{Path:item.path,Content:v2input.Ref{ArtifactID:item.staged,SHA256:item.ref.SHA256},Contract:"Override"})
  }
  raw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
  prepared,err:=canonical.Marshal(map[string]any{"padding":strings.Repeat("x",80)});if err!=nil{t.Fatal(err)}
  codec:=&expandingImportCodec{child:child,prepared:prepared}
  source:=&remapCurrentSource{}
  remapper,err:=NewRecursiveImportDependencyRemapper(uuid.New(),raw,remapArchiveStub{parentStaged:parentRaw,childStaged:childRaw},source,codec,codec);if err!=nil{t.Fatal(err)}
  budget:=int64(2*len(prepared));if mode=="overflow"{budget--}
  old,target:=v2input.SessionVersionBinding{AssetVersionID:uuid.New()},v2input.SessionVersionBinding{AssetVersionID:uuid.New()}
  var result v2input.Ref
  if mode=="public"{result,err=remapper.RemapImportRecipeDependency(context.Background(),parsed.Manifest.Entries[0].ID,parent,"Override",old,target)}else{result,err=remapper.remapImportRecipeDependency(context.Background(),parsed.Manifest.Entries[0].ID,parent,"Override",old,target,budget)}
  if mode=="overflow"{if err==nil||result!=(v2input.Ref{})||codec.published!=1{t.Fatal("oversized parent published",codec.published,err)}}else if err!=nil||!validImportRef(result)||codec.calls!=2||codec.published!=2{t.Fatal("exact/shared traversal failed",codec.calls,codec.published,err)}
 })}
}
