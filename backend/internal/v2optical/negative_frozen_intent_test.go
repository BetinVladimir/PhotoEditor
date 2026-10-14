package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type negativeFrozenDouble struct {locator negativeFrozenIntentLocator;calls int;change bool}
func(c *negativeFrozenDouble)lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,string)(negativeFrozenIntentLocator,error){c.calls++;v:=c.locator;if c.change&&c.calls>1 {v.Params.Version="changed"};return v,nil}
func TestOpticalFrozenIntentOriginalReadAndScopeRebind(t *testing.T){
 p:=perspectiveIntent();raw,_,err:=p.Compile();if err!=nil {t.Fatal(err)};sum:=sha256.Sum256(raw)
 tenant,project,reviewID,task:=uuid.New(),uuid.New(),uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};hash:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 locator:=negativeFrozenIntentLocator{SchemaVersion:1,Tenant:tenant,Project:project,Review:reviewID,Hash:hash,Task:task,Run:uuid.New(),Operation:"optical.perspective",Params:negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:]),Bucket:"photos",Key:"params",Version:"original",Bytes:int64(len(raw)),Media:"application/json"}}
 for _,mode:=range []string{"valid","version_changed","truncated","close_error","open_error_with_reader","nil_reader"}{
  t.Run(mode,func(t *testing.T){catalog:=&negativeFrozenDouble{locator:locator,change:mode=="version_changed"};reader:=&negativeArrayReadDouble{raw:raw,mode:mode}
   proof,err:=loadOpticalFrozenIntent(context.Background(),catalog,reader,who,tenant,project,reviewID,task,hash)
   if mode=="valid" {if err!=nil||catalog.calls!=2 {t.Fatal("frozen proof",err)}
    binding:=replacementBinding(p.Base);rebound,err:=proof.Rebind(context.Background(),who,tenant,project,reviewID,task,hash,[]OpticalReplacementBinding{binding},nil)
    if err!=nil||rebound.Operation()!="optical.perspective" {t.Fatal("verified rebind",err)}
    if _,err:=proof.Rebind(context.Background(),who,uuid.New(),project,reviewID,task,hash,[]OpticalReplacementBinding{binding},nil);err!=v2input.ErrInvalid {t.Fatal("foreign proof accepted",err)}
   }else if err==nil||proof.raw!="" {t.Fatal("invalid frozen bytes accepted",mode)}
   if reader.opens>0&&mode!="nil_reader"&&reader.closes!=reader.opens {t.Fatal("reader leaked")}
  })
 }
 body,_:=canonical.Marshal(locator)
 if _,err:=decodeNegativeFrozenIntent(body,"photos",tenant,project,reviewID,task,hash);err!=nil {t.Fatal("locator",err)}
 if _,err:=decodeNegativeFrozenIntent(body,"photos",tenant,project,reviewID,uuid.New(),hash);err==nil {t.Fatal("foreign task accepted")}
 locator.Operation="export.encode";body,_=canonical.Marshal(locator)
 if _,err:=decodeNegativeFrozenIntent(body,"photos",tenant,project,reviewID,task,hash);err==nil {t.Fatal("unsupported operation accepted")}
}
