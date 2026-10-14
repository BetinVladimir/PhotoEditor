package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2portrait"
)
func TestImportLocalInstallationUsesScopedPortsAndPreservesOtherOperations(t *testing.T){
 base,err:=NewImportOperationRouter(context.Background(),map[ImportOperationVersion]ImportLayerOperationQualifier{{"adjust.exposure","1"}:&preparedLayerOperationStub{}});if err!=nil{t.Fatal(err)}
 i:=ImportLocalPortraitInstallation{RuntimeSHA:rawSHA([]byte("runtime")),Capacity:v2portrait.LocalResourceEstimate{PixelCount:1,RAMBytes:1,ScratchBytes:1,OutputBytes:1}}
 masters:=&importMasterReaderStub{};documents:=&preparedLayerReaderStub{};masks:=&importMaskQualifierStub{};runtime:=&importLocalRuntimeStub{}
 installed,err:=i.Build(context.Background(),base,masters,documents,masks,runtime);if err!=nil{t.Fatal(err)}
 if len(installed.operations)!=3||len(base.operations)!=1{t.Fatal("installation changed base or lost operations")}
 for _,operation:=range []string{"portrait.hair","portrait.clothing"}{
  q,ok:=installed.operations[ImportOperationVersion{operation,"1"}].(ImportLocalPortraitOperationQualifier)
  if !ok||q.Masters!=masters||q.Documents!=documents||q.Masks!=masks||q.Runtime!=runtime||q.RuntimeSHA!=i.RuntimeSHA{t.Fatal("scoped authority replaced")}
 }
 if _,err:=i.Build(context.Background(),installed,masters,documents,masks,runtime);err==nil{t.Fatal("duplicate installed authority overwritten")}
 if _,err:=i.Build(context.Background(),nil,nil,documents,masks,runtime);err==nil{t.Fatal("missing authority installed")}
}
