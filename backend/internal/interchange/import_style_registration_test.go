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
func TestImportStyleRegistrationPreservesOwnedExactPreparedConfig(t *testing.T){
 for _,mode:=range []string{"valid","hash","uuid","style_id","background_missing","stale_background","cancelled"}{t.Run(mode,func(t *testing.T){
  scope:=preparedPublisherScope();original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("original style"))}
  raw,err:=os.ReadFile("../run/identity_style.json");if err!=nil{t.Fatal(err)}
  var fields map[string]json.RawMessage;if err:=json.Unmarshal(raw,&fields);err!=nil{t.Fatal(err)}
  styleID:=uuid.NewSHA1(scope.Binding.AssetVersionID,[]byte("interchange:style:"+original.SHA256))
  if mode=="style_id"{styleID=uuid.New()}
  fields["style_id"],err=canonical.Marshal(styleID.String());if err!=nil{t.Fatal(err)}
  if mode=="background_missing"{fields["extensions"]=json.RawMessage(`{"background":{"mode":"asset","asset_id":"foreign"}}`)}
  raw,err=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}
  prepared:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:dependency:"+scope.Entry.String()+":StyleV2:"+original.ArtifactID.String())),SHA256:rawSHA(raw)}
  if mode=="hash"{prepared.SHA256=original.SHA256};if mode=="uuid"{prepared.ArtifactID=uuid.New()}
  var background *ImportBackgroundSource;if mode=="stale_background"{background=&ImportBackgroundSource{}}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  qualifier:=&styleQualifierStub{}
  result,err:=CompileImportStyleRegistration(ctx,scope,original,prepared,raw,background,qualifier)
  if mode!="valid"{if err==nil{t.Fatal("accepted invalid registration")};return}
  if err!=nil||result.Config!=prepared||result.StyleID!=styleID||result.Original!=original||qualifier.qualified!=1||string(result.Body)!=string(raw){t.Fatal("lost exact config",err)}
  for i:=range raw{raw[i]='x'}
  if rawSHA(result.Body)!=prepared.SHA256{t.Fatal("registration aliases input bytes")}
 })}
}
