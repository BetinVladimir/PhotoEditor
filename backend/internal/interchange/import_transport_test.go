package interchange
import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func importInputFixture(t *testing.T)(ImportPreparedDocuments,[]v2input.Entry){
 docs,_:=importMaterializationFixture(t);var params struct{Parsed v2input.Ref `json:"parsed"`};if err:=json.Unmarshal(docs.params.Body,&params);err!=nil{t.Fatal(err)}
 return docs,[]v2input.Entry{{Ref:docs.plan.Ref,Contract:"InterchangePlan"},{Ref:params.Parsed,Contract:"ParsedInterchange"},{Ref:docs.params.Ref,Contract:"Params"}}
}
func TestImportInputTransportOwnsInventoryAndRejectsAliasedPublication(t *testing.T){
 docs,entries:=importInputFixture(t);tenant:=uuid.New();inventory,err:=FreezeImportInputs(tenant,docs,entries);if err!=nil{t.Fatal(err)}
 entries[0].Contract="tampered";pub:=&transportPublisher{};transport,err:=inventory.PublishInputs(context.Background(),pub)
 if err!=nil||pub.calls!=2{t.Fatal("transport publication",err)}
 if err:=transport.RegisterInputs(context.Background(),nil,tenant,docs.identity.Run);err==nil{t.Fatal("nil transaction accepted")}
 if _,err:=inventory.PublishInputs(context.Background(),&transportPublisher{collision:true,bundle:docs.plan.Ref.ArtifactID});err==nil{t.Fatal("input/transport alias accepted")}
}
func TestImportInputInventoryRejectsMissingOrSubstitutedRoots(t *testing.T){
 for _,change:=range []string{"missing","contract","sha","duplicate"}{
  docs,entries:=importInputFixture(t)
  switch change{case "missing":entries=entries[:2];case "contract":entries[0].Contract="StyleV2";case "sha":entries[0].Ref.SHA256=rawSHA([]byte("foreign"));case "duplicate":entries=append(entries,entries[0])}
  if _,err:=FreezeImportInputs(uuid.New(),docs,entries);err==nil{t.Fatal("invalid inventory accepted",change)}
 }
}
