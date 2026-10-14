package interchange
import (
 "context"
 "testing"
)
func TestImportCanonicalWarpReaderRequiresNativeQualificationAndMetadataRecheck(t *testing.T){
 for _,role:=range []string{"DenseWarpField"}{
  entry,catalog,objects:=importOpaqueFixture();entry.Contract=role
  node,err:=(ImportCanonicalWarpNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024,Qualifier:&exportBinaryQualifierStub{}}).ResolveImportNode(context.Background(),entry)
  if err!=nil||node.Entry!=entry||catalog.calls!=2{t.Fatal("pixel verification",role,err)}
 }
}
func TestImportCanonicalWarpReaderRejectsOpaqueRoleBeforeIO(t *testing.T){
 entry,catalog,objects:=importOpaqueFixture()
 if _,err:=(ImportCanonicalWarpNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024}).ResolveImportNode(context.Background(),entry);err==nil||objects.opens!=0{t.Fatal("opaque role accepted by canonical reader")}
}
func TestImportResolverRouterFailsClosedWithoutWarpInstallation(t *testing.T){
 entry,_,_:=importOpaqueFixture();entry.Contract="DenseWarpField"
 if _,err:=(ImportNodeResolverRouter{}).ResolveImportNode(context.Background(),entry);err==nil{t.Fatal("missing pixel resolver accepted")}
}
func TestImportCanonicalWarpRejectsMissingNativeBeforeIO(t *testing.T){
 entry,catalog,objects:=importOpaqueFixture();entry.Contract="DenseWarpField"
 if _,err:=(ImportCanonicalWarpNodeResolver{Catalog:catalog,Objects:objects,MaxBytes:1024}).ResolveImportNode(context.Background(),entry);err==nil||catalog.calls!=0||objects.opens!=0{t.Fatal("metadata-only warp accepted")}
}
