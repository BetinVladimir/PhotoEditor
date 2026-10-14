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
type adoptedParentDouble struct{value adoptedOpticalParent;calls int;change bool}
func(c *adoptedParentDouble)lookup(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID)(adoptedOpticalParent,error){c.calls++;v:=c.value;if c.change&&c.calls>1{v.Result=uuid.New()};return v,nil}
func TestAdoptedOpticalParentPhysicalDocumentAndAuthority(t *testing.T){
 _,source,_,_:=negativeDraftFixture(t);sum:=sha256.Sum256(source)
 value:=adoptedOpticalParent{SchemaVersion:1,Tenant:uuid.New(),Project:uuid.New(),Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1,Operation:"optical.negative",Source:negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:]),Bucket:"photos",Key:"source",Version:"original",Bytes:int64(len(source)),Media:"application/json"}}
 for _,mode:=range []string{"valid","authority_changed","truncated","close_error","open_error_with_reader","nil_reader","wrong_hash","bad_scope","unbounded"}{t.Run(mode,func(t *testing.T){
  catalog:=&adoptedParentDouble{value:value,change:mode=="authority_changed"};reader:=&negativeArrayReadDouble{raw:source,mode:mode}
  if mode=="wrong_hash"{reader.raw=append([]byte(nil),source...);reader.raw[len(source)-1]=' '}
  if mode=="bad_scope"{catalog.value.Tenant=uuid.New()};if mode=="unbounded"{catalog.value.Source.Bytes=1<<60}
  got,err:=loadAdoptedOpticalParent(context.Background(),catalog,reader,auth.Identity{},value.Tenant,value.Project,value.Task,value.Run)
  if mode=="valid"{
   if err!=nil||catalog.calls!=2||string(got.WorkingImageBytes())!=string(source)||got.SourceRef().SHA256!=value.Source.SHA256{t.Fatal("parent proof",err)}
   raw:=got.WorkingImageBytes();raw[0]='[';if got.WorkingImageBytes()[0]=='['{t.Fatal("mutable original bytes")}
  }else if err==nil||got.SourceRef().ArtifactID!=uuid.Nil{t.Fatal("invalid parent accepted",mode)}
  if (mode=="bad_scope"||mode=="unbounded")&&reader.opens!=0{t.Fatal("unvalidated locator reached object IO")}
  if reader.opens>0&&mode!="nil_reader"&&reader.opens!=reader.closes{t.Fatal("reader leaked")}
 })}
 packet,_:=canonical.Marshal(value)
 if _,err:=decodeAdoptedOpticalParent(packet,"other",value.Tenant,value.Project,value.Task,value.Run);err==nil{t.Fatal("wrong bucket accepted")}
 value.Attempt=0;packet,_=canonical.Marshal(value)
 if _,err:=decodeAdoptedOpticalParent(packet,"photos",value.Tenant,value.Project,value.Task,value.Run);err==nil{t.Fatal("invalid attempt accepted")}
}
