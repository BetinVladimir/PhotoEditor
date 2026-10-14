package interchange
import (
 "archive/zip"
 "context"
 "os"
 "sort"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func TestExportArchiveReaderStagesValidatesAndCleansScratch(t *testing.T){
 intent,compiled,snapshot,raw:=exportFreezeFixture(t);var err error
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{t.Fatal(err)}
 imageBytes:=exportTIFFTestBytes(1)
 want:=plan.Entries[0];want.Content.SHA256=rawSHA(imageBytes)
 compiled,err=CompileExportPlan(context.Background(),intent.Identity.Workflow,intent.Request.ProjectID,"xmp",snapshot,raw,ExportLimits{1<<20,1<<20,10},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return ExportFrame{ExternalID:want.External,Path:want.Path,Content:want.Content,PortableRecipe:*want.Recipe,Rating:want.Rating,Pick:want.Pick,Crop:want.Crop},nil}));if err!=nil{t.Fatal(err)}
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 sidecarPath:="images/one.xmp";payload:=exportXMPTestPayload(want,1);sidecarSHA:=rawSHA([]byte(payload))
 manifest:=exportBundleManifest{Schema:1,Renderer:"v2.2",Entries:[]exportBundleEntry{{ID:want.ID,External:want.External,ImagePath:want.Path,ImageSHA:want.Content.SHA256,Rating:want.Rating,Pick:want.Pick,Crop:want.Crop}},Masks:[]exportBundleMask{},Inventory:[]exportBundleInventoryEntry{{ID:want.Content.ArtifactID,Path:want.Path,SHA:want.Content.SHA256,Contract:"EncodedImage"},{ID:uuid.NewSHA1(want.ID,[]byte("interchange:xmp:"+sidecarSHA)),Path:sidecarPath,SHA:sidecarSHA,Contract:"XMP"}}}
 sort.Slice(manifest.Inventory,func(i,j int)bool{return manifest.Inventory[i].Path<manifest.Inventory[j].Path})
 body,err:=canonical.Marshal(manifest);if err!=nil{t.Fatal(err)}
 archive:=exportZIPTestBytes(t,[]exportZIPTestFile{{want.Path,string(imageBytes),zip.Store},{sidecarPath,payload,zip.Deflate},{"manifest.json",string(body),zip.Deflate}})
 for _,kind:=range []string{"valid","hash","version","scratch","zip"}{t.Run(kind,func(t *testing.T){
  root:=t.TempDir();maximum:=int64(1<<20);if kind=="scratch"{maximum=23}
  scratch,err:=NewExportArchiveScratch(root,maximum);if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(archive)}
  catalog:=&exportBundleCatalogStub{object:objectstore.Object{Key:"bundle",VersionID:"v1",ContentType:"application/zip",SHA256:ref.SHA256,Bytes:int64(len(archive))}}
  objects:=&exportBundleObjectsStub{body:append([]byte(nil),archive...)}
  if kind=="hash"{objects.body[0]='x'};if kind=="version"{catalog.change=true}
  if kind=="zip"{objects.body[len(objects.body)-22]='x';ref.SHA256=rawSHA(objects.body);catalog.object.SHA256=ref.SHA256}
  result,err:=(ExportArchiveReader{Catalog:catalog,Objects:objects,Scratch:scratch}).Read(context.Background(),intent,frozen,ref)
  if kind=="valid"{
   if err!=nil||result.Bundle()!=ref||len(result.Files())!=3||catalog.calls!=4{t.Fatal("valid archive refused",err)}
   owned:=result.Manifest();owned[0]='x';if result.Manifest()[0]=='x'{t.Fatal("manifest alias")}
  }else if err==nil{t.Fatal("invalid archive accepted")}
  entries,err:=os.ReadDir(root);if err!=nil||len(entries)!=0||scratch.used!=0||objects.opens!=objects.closes{t.Fatal("scratch/stream leaked",err)}
 })}
}
func TestExportArchiveScratchEnforcesSharedReservations(t *testing.T){
 scratch,err:=NewExportArchiveScratch(t.TempDir(),100);if err!=nil{t.Fatal(err)}
 if !scratch.reserve(60)||scratch.reserve(41)||scratch.reserve(-1){t.Fatal("invalid reservation")}
 scratch.release(60);if !scratch.reserve(100){t.Fatal("capacity not restored")};scratch.release(100)
 if _,err:=NewExportArchiveScratch("relative",100);err==nil{t.Fatal("relative scratch accepted")}
}
