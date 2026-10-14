package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
)
type adoptedArrayDouble struct{object objectstore.Object;calls int;changed bool}
func(c *adoptedArrayDouble)resolve(context.Context,auth.Identity,VerifiedAdoptedOpticalParent,NegativeSourceArraySpec)(objectstore.Object,error){c.calls++;o:=c.object;if c.changed&&c.calls>1{o.VersionID="changed"};return o,nil}
func TestAdoptedParentArraysHDRAndStableVersions(t *testing.T){
 _,source,_,_:=negativeDraftFixture(t);spec,object,pixels:=perspectiveArrayFixture(t,3,[]float32{-.25,1.5,.4})
 image,err:=review.ParseWorkingImage(source);if err!=nil{t.Fatal(err)}
 image.Canvas.Width=1;image.Canvas.Height=1;image.Alpha=nil;image.Pixels.ID=spec.Ref.ArtifactID;image.Pixels.SHA256=spec.Ref.SHA256
 source,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(source)
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 v:=VerifiedAdoptedOpticalParent{metadata:adoptedOpticalParent{SchemaVersion:1,Tenant:uuid.New(),Project:uuid.New(),Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1,Operation:"optical.negative",Source:negativeSourceLocator{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:]),Bucket:"photos",Key:"source",Version:"original",Bytes:int64(len(source)),Media:"application/json"}},raw:string(source),who:who}
 for _,mode:=range []string{"valid","version_changed","truncated","close_error","open_error_with_reader","nil_reader","foreign_identity"}{t.Run(mode,func(t *testing.T){
  catalog:=&adoptedArrayDouble{object:object,changed:mode=="version_changed"};reader:=&negativeArrayReadDouble{raw:pixels,mode:mode};actor:=who;if mode=="foreign_identity"{actor.SessionID=uuid.New()}
  proof,err:=loadAdoptedParentArrays(context.Background(),catalog,reader,actor,v)
  if mode=="valid"{if err!=nil||len(proof.objects)!=1||proof.objects[0]!=object||catalog.calls!=3{t.Fatal("HDR proof",err)}
   if _,_,err:=adoptedParentProofPacket(context.Background(),"photos",who,v.metadata.Tenant,v.metadata.Project,proof);err!=nil{t.Fatal("proof packet",err)}
   if _,_,err:=adoptedParentProofPacket(context.Background(),"photos",who,uuid.New(),v.metadata.Project,proof);err==nil{t.Fatal("foreign tenant packet accepted")}
   if err:=LockAdoptedParentArrays(context.Background(),nil,"photos",who,v.metadata.Tenant,v.metadata.Project,proof);err==nil{t.Fatal("nil transaction accepted")}
   scope:=PreparedNegativeReplacementInputs{bucket:"photos",who:who,tenant:v.metadata.Tenant,project:v.metadata.Project}
   for _,mode:=range []string{"mime","version","sha","size"}{
    bad:=proof;bad.objects=append([]objectstore.Object(nil),proof.objects...)
    switch mode{case "mime":bad.objects[0].ContentType="text/plain";case "version":bad.objects[0].VersionID="null";case "sha":bad.objects[0].SHA256="bad";case "size":bad.objects[0].Bytes=0}
    if err:=validAdoptedParentProof(context.Background(),scope,bad);err==nil{t.Fatal("invalid parent locator accepted",mode)}
   }

}else if err==nil||len(proof.objects)!=0{t.Fatal("invalid arrays accepted",mode)}
  if mode=="foreign_identity"&&(reader.opens!=0||catalog.calls!=0){t.Fatal("foreign session reached IO")}
  if reader.opens>0&&mode!="nil_reader"&&reader.opens!=reader.closes{t.Fatal("reader leaked")}
 })}
 v.raw+=" ";if _,err:=adoptedParentArraySpecs(v);err==nil{t.Fatal("tampered parent bytes accepted")}
}
