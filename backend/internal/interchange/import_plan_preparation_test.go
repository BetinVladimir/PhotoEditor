package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type importSourceReaderStub struct{inventory ImportSourceInventory;calls int;change bool;formatChange string;fail error}
func(s *importSourceReaderStub)Read(_ context.Context,_ v2input.Ref,_ []byte)(ImportSourceInventory,error){
 s.calls++;if s.calls>1&&s.fail!=nil{return ImportSourceInventory{},s.fail}
 result:=s.inventory;if s.change&&s.calls>1{result.body=[]byte(`{}`)}
 if s.calls>1&&s.formatChange!=""{
  result.document.Format=s.formatChange
  raw,err:=canonical.Marshal(result.document);if err!=nil{return ImportSourceInventory{},err};result.body=raw
 }
 return result,nil
}
func TestImportPlanPreparationRequiresUnchangedCurrentSources(t *testing.T){
 for _,change:=range []string{"none","changed","format","denied"}{
  workflow,project,ref,parsed,sources:=importSourcesFixture(t);if change=="format"{sources.Format="xmp"}
  raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
  frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);if err!=nil{t.Fatal(err)}
  reader:=&importSourceReaderStub{inventory:frozen,change:change=="changed"};if change=="format"{reader.formatChange="captureone_sidecar"}
  sentinel:=errors.New("current ACL denied");if change=="denied"{reader.fail=sentinel}
  _,err=(ImportPlanPreparation{Sources:reader}).Compile(context.Background(),ref,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10})
  if reader.calls!=2{t.Fatal("missing authority recheck")}
  if change=="none"&&err!=nil{t.Fatal(err)}
  if (change=="changed"||change=="format")&&!errors.Is(err,artifactjson.ErrStale){t.Fatal("changed source accepted",err)}
  if change=="denied"&&!errors.Is(err,sentinel){t.Fatal("authority refusal lost",err)}
 }
}
