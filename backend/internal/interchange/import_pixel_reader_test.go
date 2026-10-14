package interchange
import (
 "context"
 "testing"
)
func TestImportCanonicalPixelReaderPreservesRoleAndChecksBytes(t *testing.T){
 for _,role:=range []string{"WorkingPixels","WorkingAlpha"}{
  entry,catalog,objects:=importOpaqueFixture();entry.Contract=role
  node,err:=(ImportCanonicalPixelNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024}).ResolveImportNode(context.Background(),entry)
  if err!=nil||node.Entry!=entry||catalog.calls!=2{t.Fatal("pixel verification",role,err)}
 }
}
func TestImportCanonicalPixelReaderRejectsOpaqueRoleBeforeIO(t *testing.T){
 entry,catalog,objects:=importOpaqueFixture()
 if _,err:=(ImportCanonicalPixelNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024}).ResolveImportNode(context.Background(),entry);err==nil||objects.opens!=0{t.Fatal("opaque role accepted by canonical reader")}
}
func TestImportResolverRouterFailsClosedWithoutPixelInstallation(t *testing.T){
 entry,_,_:=importOpaqueFixture();entry.Contract="WorkingPixels"
 if _,err:=(ImportNodeResolverRouter{}).ResolveImportNode(context.Background(),entry);err==nil{t.Fatal("missing pixel resolver accepted")}
}
