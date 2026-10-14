package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestImportOperationRouterExactInstalledVersionAndOwnership(t *testing.T){
 key:=ImportOperationVersion{"adjust.exposure","1"}
 installed:=map[ImportOperationVersion]ImportLayerOperationQualifier{key:&preparedLayerOperationStub{}}
 selected:=installed[key].(*preparedLayerOperationStub)
 router,err:=NewImportOperationRouter(context.Background(),installed);if err!=nil{t.Fatal(err)}
 delete(installed,key)
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("source"))},CanvasHash:rawSHA([]byte("canvas"))}
 raw:=[]byte(`{"schema_version":1}`)
 if err:=router.QualifyImportedLayerOperation(context.Background(),key.Operation,key.Version,raw,binding);err!=nil{t.Fatal(err)}
 if selected.calls!=1||raw[0]!='{'{t.Fatal("configuration or caller buffer aliased")}
 for _,unknown:=range []ImportOperationVersion{{key.Operation,"2"},{"unknown","1"},{" adjust.exposure","1"}}{
  if err:=router.QualifyImportedLayerOperation(context.Background(),unknown.Operation,unknown.Version,raw,binding);err==nil{t.Fatal("unknown operation dispatched")}
 }
 if selected.calls!=1{t.Fatal("unknown operation reached validator")}
 selected.failure=errors.New("runtime revoked")
 if err:=router.QualifyImportedLayerOperation(context.Background(),key.Operation,key.Version,raw,binding);!errors.Is(err,selected.failure){t.Fatal("validator refusal lost",err)}
 if err:=router.QualifyImportedLayerOperation(context.Background(),key.Operation,key.Version,[]byte(`{ "schema_version":1 }`),binding);err==nil{t.Fatal("noncanonical Params accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=router.QualifyImportedLayerOperation(ctx,key.Operation,key.Version,raw,binding);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
func TestImportOperationRouterRejectsIncompleteInstallation(t *testing.T){
 for _,installed:=range []map[ImportOperationVersion]ImportLayerOperationQualifier{nil,{{"x","1"}:nil},{{"","1"}:&preparedLayerOperationStub{}},{{"x"," "}:&preparedLayerOperationStub{}}}{
  if _,err:=NewImportOperationRouter(context.Background(),installed);err==nil{t.Fatal("incomplete installation accepted")}
 }
}
