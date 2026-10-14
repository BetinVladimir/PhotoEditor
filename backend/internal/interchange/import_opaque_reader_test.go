package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
func importOpaqueFixture()(v2input.Entry,*importCatalogStub,*importReaderStub){
 raw:=[]byte("opaque-original");entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"Artifact"}
 return entry,&importCatalogStub{object:objectstore.Object{Key:"key",VersionID:"version-1",SHA256:entry.Ref.SHA256,Bytes:int64(len(raw)),ContentType:"application/octet-stream"}},&importReaderStub{body:raw}
}
func TestImportOpaqueReaderVerifiesActualBytesAndRechecksScope(t *testing.T){
 entry,catalog,objects:=importOpaqueFixture();node,err:=(ImportOpaqueNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024}).ResolveImportNode(context.Background(),entry)
 if err!=nil||node.Entry!=entry||node.Body!=nil||len(node.Children)!=0||catalog.calls!=2{t.Fatal("opaque verification",err)}
}
func TestImportOpaqueReaderRejectsSubstitutionTruncationAndOversize(t *testing.T){
 for _,change:=range []string{"hash","short","extra","limit","version"}{
  entry,catalog,objects:=importOpaqueFixture();limit:=int64(1024)
  switch change{case "hash":objects.body[0]='x';case "short":objects.body=objects.body[:2];case "extra":objects.body=append(objects.body,'x');case "limit":limit=2;case "version":catalog.change=true}
  if _,err:=(ImportOpaqueNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:limit}).ResolveImportNode(context.Background(),entry);err==nil{t.Fatal("invalid opaque accepted",change)}
 }
}
func TestImportOpaqueReaderDoesNotGrantTypedAuthority(t *testing.T){
 entry,catalog,objects:=importOpaqueFixture();entry.Contract="MaskPixels"
 if _,err:=(ImportOpaqueNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024}).ResolveImportNode(context.Background(),entry);err==nil||objects.opens!=0{t.Fatal("opaque became typed mask")}
}
