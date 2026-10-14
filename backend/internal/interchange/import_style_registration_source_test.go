package interchange

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestStyleRegistrationOriginalComesFromParsedInventory(t *testing.T){
 scope:=preparedPublisherScope();original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("original"))}
 parsed:=importParsedValidatorFixture()
 parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:original.ArtifactID,Path:"dependencies/style.json",SHA:original.SHA256,Contract:"StyleV2"})
 parsed.Files=append(parsed.Files,importParsedFile{Path:"dependencies/style.json",Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:original.SHA256},Contract:"StyleV2"})
 parsedRaw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile("../run/identity_style.json");if err!=nil{t.Fatal(err)}
 var fields map[string]json.RawMessage;if err:=json.Unmarshal(raw,&fields);err!=nil{t.Fatal(err)}
 fields["style_id"],err=canonical.Marshal(uuid.NewSHA1(scope.Binding.AssetVersionID,[]byte("interchange:style:"+original.SHA256)).String());if err!=nil{t.Fatal(err)}
 raw,err=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:dependency:"+scope.Entry.String()+":StyleV2:"+original.ArtifactID.String())),SHA256:rawSHA(raw)}
 result,err:=PrepareImportStyleRegistration(context.Background(),scope,parsedRaw,ref,raw,&styleQualifierStub{})
 if err!=nil||result.Original!=original||result.Config!=ref{t.Fatal("lost inventory authority",err)}
 changed:=scope;changed.Entry=uuid.New()
 if _,err:=PrepareImportStyleRegistration(context.Background(),changed,parsedRaw,ref,raw,&styleQualifierStub{});err==nil{t.Fatal("accepted different entry")}
 parsed.Manifest.Inventory[len(parsed.Manifest.Inventory)-1].Contract="Params";parsed.Files[len(parsed.Files)-1].Contract="Params"
 parsedRaw,err=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
 if _,err:=PrepareImportStyleRegistration(context.Background(),scope,parsedRaw,ref,raw,&styleQualifierStub{});err==nil{t.Fatal("accepted relabeled original")}
}
