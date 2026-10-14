package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)
type negativeSelectionDouble struct{value negativeSelectedSource;calls int;change bool}
func(c *negativeSelectionDouble)lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,NegativeDeclinedDecision)(negativeSelectedSource,error){
 c.calls++;v:=c.value;if v.Source!=nil {owned:=*v.Source;v.Source=&owned;if c.change&&c.calls>1 {v.Source.Version="changed"}};return v,nil
}
func TestNegativeSelectedSourcePhysicalReadAndRecheck(t *testing.T){
 _,source,_,_:=negativeDraftFixture(t)
 sum:=sha256.Sum256(source);hash:=hex.EncodeToString(sum[:])
 tenant,project:=uuid.New(),uuid.New();d:=NegativeDeclinedDecision{Review:uuid.New(),OriginTask:uuid.New(),OriginRun:uuid.New(),DecisionHash:hash,Route:"base"}
 value:=negativeSelectedSource{SchemaVersion:1,Tenant:tenant,Project:project,Review:d.Review,Task:d.OriginTask,Run:d.OriginRun,Hash:hash,Route:"base",Source:&negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hash,Bucket:"photos",Key:"source",Version:"original",Bytes:int64(len(source)),Media:"application/json"}}
 for _,mode:=range []string{"valid","version_changed","truncated","close_error","open_error_with_reader","nil_reader","wrong_hash"} {
  t.Run(mode,func(t *testing.T){catalog:=&negativeSelectionDouble{value:value,change:mode=="version_changed"};reader:=&negativeArrayReadDouble{raw:source,mode:mode}
   if mode=="wrong_hash" {reader.raw=append([]byte(nil),source...);reader.raw[len(source)-1]=' '}
   got,err:=loadNegativeSelectedSource(context.Background(),catalog,reader,auth.Identity{},tenant,project,d)
   if mode=="valid" {if err!=nil||got.SelectedBase()==nil||got.SelectedBase().SHA256!=hash||string(got.WorkingImageBytes())!=string(source)||catalog.calls!=2 {t.Fatal("selection proof",err)}
    changed:=got.WorkingImageBytes();changed[0]='[';ref:=got.SelectedBase();ref.SHA256="changed";if got.WorkingImageBytes()[0]=='['||got.SelectedBase().SHA256!=hash {t.Fatal("mutable proof")}
   } else if err==nil||got.SelectedBase()!=nil {t.Fatal("invalid selection accepted",mode)}
   if reader.opens>0&&mode!="nil_reader"&&reader.closes!=reader.opens {t.Fatal("reader leaked")}
  })
 }
 value.Route="skip";value.Source=nil;d.Route="skip"
 reader:=&negativeArrayReadDouble{};catalog:=&negativeSelectionDouble{value:value}
 got,err:=loadNegativeSelectedSource(context.Background(),catalog,reader,auth.Identity{},tenant,project,d)
 if err!=nil||got.SelectedBase()!=nil||len(got.WorkingImageBytes())!=0||reader.opens!=0||catalog.calls!=2 {t.Fatal("skip synthesized source",err)}
}
func TestNegativeSelectedSourceWireBindsExactDecisionAndScope(t *testing.T){
 tenant,project:=uuid.New(),uuid.New();d:=NegativeDeclinedDecision{Review:uuid.New(),OriginTask:uuid.New(),OriginRun:uuid.New(),DecisionHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Route:"skip"}
 wire:=negativeSelectedSource{SchemaVersion:1,Tenant:tenant,Project:project,Review:d.Review,Task:d.OriginTask,Run:d.OriginRun,Hash:d.DecisionHash,Route:d.Route}
 raw,_:=canonical.Marshal(wire)
 if _,err:=decodeNegativeSelectedSource(raw,tenant,project,d,"photos");err!=nil {t.Fatal(err)}
 if _,err:=decodeNegativeSelectedSource(raw,uuid.New(),project,d,"photos");err==nil {t.Fatal("cross tenant accepted")}
 d.DecisionHash="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
 if _,err:=decodeNegativeSelectedSource(raw,tenant,project,d,"photos");err==nil {t.Fatal("different decision accepted")}
}
