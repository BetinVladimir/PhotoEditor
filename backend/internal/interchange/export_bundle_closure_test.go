package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestExportBundleClosureInventoryRequiresExactQualifiedRefs(t *testing.T){
 intent,compiled,snapshot,raw:=exportFreezeFixture(t)
 intent.Request.Format="photoeditor_bundle";intent.RequestJCS,_=canonical.Marshal(intent.Request);intent.RequestSHA=rawSHA(intent.RequestJCS)
 var old importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&old);err!=nil{t.Fatal(err)};item:=old.Entries[0]
 var err error
 compiled,err=CompileExportPlan(context.Background(),intent.Identity.Workflow,intent.Request.ProjectID,"photoeditor_bundle",snapshot,raw,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return ExportFrame{ExternalID:item.External,Path:item.Path,Content:item.Content,PortableRecipe:*item.Recipe,Rating:item.Rating,Pick:item.Pick,Crop:item.Crop},nil}));if err!=nil{t.Fatal(err)}
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 entries:=[]v2input.Entry{{Ref:item.Content,Contract:"EncodedImage"},{Ref:*item.Recipe,Contract:"EditingRecipeV3"}}
 digest,err:=canonical.Hash(entries);if err!=nil{t.Fatal(err)}
 inventory:=ExportInputInventory{task:compiled.task,entries:entries,digest:digest}
 manifest:=exportBundleManifest{Schema:1,Renderer:"v2.2",Entries:[]exportBundleEntry{},Masks:[]exportBundleMask{},Inventory:[]exportBundleInventoryEntry{{ID:item.Content.ArtifactID,Path:item.Path,SHA:item.Content.SHA256,Contract:"EncodedImage"},{ID:item.Recipe.ArtifactID,Path:"recipes/"+item.Recipe.ArtifactID.String()+".json",SHA:item.Recipe.SHA256,Contract:"EditingRecipeV3"}}}
 body,_:=canonical.Marshal(manifest)
 if err:=ValidateExportBundleSourceInventory(context.Background(),intent,frozen,body,inventory);err!=nil{t.Fatal("exact source inventory refused",err)}
 for _,kind:=range []string{"digest","path","contract","missing","unexpected_background","background_digest"}{t.Run(kind,func(t *testing.T){
  checked:=inventory;changed:=manifest;changed.Inventory=append([]exportBundleInventoryEntry(nil),manifest.Inventory...)
  switch kind{case "digest":checked.digest="wrong";case "path":changed.Inventory[0].Path="wrong.tiff";case "contract":changed.Inventory[0].Contract="WorkingImage";case "missing":changed.Inventory=changed.Inventory[:1]}
  if kind=="unexpected_background"||kind=="background_digest"{
   changed.Backgrounds=[]bundleBackground{{Style:*item.Recipe,Portable:"studio",Path:item.Path,SHA:item.Content.SHA256}}
   if kind=="background_digest"{checked.backgrounds=append([]bundleBackground(nil),changed.Backgrounds...);checked.backgroundDigest="invalid"}
  }
  bytes,_:=canonical.Marshal(changed)
  if err:=ValidateExportBundleSourceInventory(context.Background(),intent,frozen,bytes,checked);err==nil{t.Fatal("invalid closure accepted")}
 })}
}
