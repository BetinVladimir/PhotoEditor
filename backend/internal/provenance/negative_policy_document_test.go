package provenance

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

func negativePolicyDocumentFixture(t *testing.T)([]byte,v2optical.NegativeQualificationPin){
 t.Helper();raw,err:=os.ReadFile("../v2optical/testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var fixture struct{Pin v2optical.NegativeQualificationPin;Locks v2qualification.DeploymentLocks}
 if json.Unmarshal(raw,&fixture)!=nil{t.Fatal("bad fixture")}
 body,err:=canonical.Marshal(SigningNegativePolicyDocument{Schema:"signing-negative-policies/v1",Policies:[]SigningNegativePolicyDocumentEntry{{PolicyID:fixture.Pin.PolicyID,PolicySHA:fixture.Pin.PolicySHA,Locks:fixture.Locks}}});if err!=nil{t.Fatal(err)}
 return body,fixture.Pin
}
func TestNegativePolicyDocumentRequiresPinnedCanonicalClosedShape(t *testing.T){
 original,pin:=negativePolicyDocumentFixture(t)
 for _,mode:=range []string{"valid","wrong_pin","whitespace","unknown","duplicate","empty","unlicensed","cancelled"}{
  t.Run(mode,func(t *testing.T){
   raw:=append([]byte(nil),original...)
   switch mode{
   case "whitespace":raw=append(raw,'\n')
   case "unknown":var fields map[string]any;_ = json.Unmarshal(raw,&fields);fields["extra"]=true;raw,_=canonical.Marshal(fields)
   case "duplicate":raw=append([]byte(`{"schema":"other",`),raw[1:]...)
   case "empty":raw=[]byte(`{"policies":[],"schema":"signing-negative-policies/v1"}`)
   case "unlicensed":raw=[]byte(strings.Replace(string(raw),`"license_cleared":true`,`"license_cleared":false`,1))
   }
   sha:=bodyDigest(raw);if mode=="wrong_pin"{sha=strings.Repeat("f",64)}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
   registry,err:=DecodeSigningNegativePolicyDocument(ctx,raw,sha)
   if mode=="valid"{if err!=nil{t.Fatal(err)};if _,err:=registry.Resolve(ctx,pin);err!=nil{t.Fatal(err)};return}
   if err==nil{t.Fatal("invalid provisioning accepted",mode)}
  })
 }
}
func TestNegativePolicyFileRejectsSymlinkAndWritableFile(t *testing.T){
 raw,pin:=negativePolicyDocumentFixture(t);path:=filepath.Join(t.TempDir(),"negative-policies.json")
 if err:=os.WriteFile(path,raw,0600);err!=nil{t.Fatal(err)}
 registry,err:=LoadSigningNegativePolicyDocument(context.Background(),path,bodyDigest(raw));if err!=nil{t.Fatal(err)}
 if _,err:=registry.Resolve(context.Background(),pin);err!=nil{t.Fatal(err)}
 link:=path+".link";if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=LoadSigningNegativePolicyDocument(context.Background(),link,bodyDigest(raw));err==nil{t.Fatal("symlink accepted")}
 if err:=os.Chmod(path,0666);err!=nil{t.Fatal(err)}
 if _,err:=LoadSigningNegativePolicyDocument(context.Background(),path,bodyDigest(raw));err==nil{t.Fatal("writable policy file accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=LoadSigningNegativePolicyDocument(ctx,path,bodyDigest(raw));!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
