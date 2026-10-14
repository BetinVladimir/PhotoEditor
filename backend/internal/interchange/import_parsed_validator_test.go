package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func importParsedValidatorFixture()importParsedDocument{
 hash:=rawSHA([]byte("original"));path:="originals/a.CR3"
 return importParsedDocument{Schema:1,Manifest:importBundleManifest{Schema:1,Renderer:"v2.2",Masks:[]importBundleMask{},Inventory:[]importBundleInventory{{ID:uuid.New(),Path:path,SHA:hash,Contract:"EncodedImage"}},Entries:[]importBundleEntry{{ID:uuid.New(),External:"frame-1",ImagePath:path,ImageSHA:hash,Rating:0,Pick:"unmarked"}}},Files:[]importParsedFile{{Path:path,Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:hash},Contract:"EncodedImage"}}}
}
func TestParsedImportValidatorRetainsEveryFileAsOpaqueInput(t *testing.T){
 document:=importParsedValidatorFixture();raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
 children,err:=validateParsedImportDocument(context.Background(),raw)
 if err!=nil||len(children)!=1||children[0].Contract!="Artifact"||children[0].Ref!=document.Files[0].Content{t.Fatal("parsed document",err)}
}
func TestParsedImportValidatorRejectsInventorySubstitutionAndGaps(t *testing.T){
 for _,change:=range []string{"hash","missing","contract","duplicate","entry"}{
  document:=importParsedValidatorFixture()
  switch change{case "hash":document.Files[0].Content.SHA256=rawSHA([]byte("foreign"));case "missing":document.Files=nil;case "contract":document.Files[0].Contract="StyleV2";case "duplicate":document.Manifest.Entries=append(document.Manifest.Entries,document.Manifest.Entries[0]);case "entry":document.Manifest.Entries[0].ImagePath="missing.CR3"}
  raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
  if _,err:=validateParsedImportDocument(context.Background(),raw);err==nil{t.Fatal("invalid parsed accepted",change)}
 }
}
func TestParsedImportPersonOriginMappingRequiresExactInventory(t *testing.T){
 for _,change:=range []string{"valid","receipt","entry","recipe","override","mask","pixels","duplicate"}{
  document:=importParsedValidatorFixture()
  add:=func(contract string)v2input.Ref{
   digest:=rawSHA([]byte(contract));id:=uuid.New();path:="dependencies/"+id.String()+".json"
   document.Manifest.Inventory=append(document.Manifest.Inventory,importBundleInventory{ID:id,Path:path,SHA:digest,Contract:contract})
   document.Files=append(document.Files,importParsedFile{Path:path,Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest},Contract:contract})
   return v2input.Ref{ArtifactID:id,SHA256:digest}
  }
  recipe:=add("EditingRecipeV3");override:=add("Override");mask:=add("MaskRef");pixels:=add("MaskPixels");receipt:=add("PersonOriginReceipt")
  recipePath:="dependencies/"+recipe.ArtifactID.String()+".json";recipeSHA:=recipe.SHA256
  document.Manifest.Entries[0].RecipePath=&recipePath;document.Manifest.Entries[0].RecipeSHA=&recipeSHA
  origin:=importBundlePersonOrigin{EntryID:document.Manifest.Entries[0].ID,Recipe:recipe,Override:override,
   MaskRef:mask,MaskPixels:pixels,Path:"dependencies/"+receipt.ArtifactID.String()+".json",SHA:receipt.SHA256}
  switch change{
  case "receipt":origin.Path="dependencies/"+override.ArtifactID.String()+".json"
  case "entry":origin.EntryID=uuid.New()
  case "recipe":origin.Recipe.SHA256=rawSHA([]byte("other"))
  case "override":origin.Override.ArtifactID=uuid.New()
  case "mask":origin.MaskRef.SHA256=rawSHA([]byte("other"))
  case "pixels":origin.MaskPixels.ArtifactID=uuid.New()
  }
  document.Manifest.PersonOrigins=[]importBundlePersonOrigin{origin}
  if change=="duplicate"{document.Manifest.PersonOrigins=append(document.Manifest.PersonOrigins,origin)}
  raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
  _,err=validateParsedImportDocument(context.Background(),raw)
  if change=="valid"&&err!=nil{t.Fatal("valid origin mapping refused",err)}
  if change!="valid"&&err==nil{t.Fatal("invalid origin mapping admitted",change)}
 }
}
