package workflow

import (
 "context"
 "encoding/json"
 "testing"
 "photoeditor/backend/internal/canonical"
)

func TestDownloadLoaderPreservesFullPresetNamingHash(t *testing.T){
 loader,tenant,run,db:=downloadLoaderFixture(t)
 var envelope downloadPresetEnvelope
 if err:=json.Unmarshal(db.contextRows.values[0][4].([]byte),&envelope);err!=nil{t.Fatal(err)}
 envelope.Config.NamingTemplate="{original_name}_edited_{sha8}"
 envelope.Hash,_=canonical.Hash(envelope.Config)
 db.contextRows.values[0][4],_=canonical.Marshal(envelope)
 batch,err:=loader.Load(context.Background(),tenant,run)
 if err!=nil||batch.Spec.Preset.NamingTemplate!=envelope.Config.NamingTemplate{t.Fatal(batch,err)}
}
func TestDownloadNamingChangeCannotReuseFrozenHash(t *testing.T){
 loader,tenant,run,db:=downloadLoaderFixture(t)
 var envelope downloadPresetEnvelope
 if err:=json.Unmarshal(db.contextRows.values[0][4].([]byte),&envelope);err!=nil{t.Fatal(err)}
 envelope.Config.NamingTemplate="different"
 db.contextRows.values[0][4],_=canonical.Marshal(envelope)
 if _,err:=loader.Load(context.Background(),tenant,run);err==nil{t.Fatal("changed naming accepted under old hash")}
}
