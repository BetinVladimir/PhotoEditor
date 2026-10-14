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
