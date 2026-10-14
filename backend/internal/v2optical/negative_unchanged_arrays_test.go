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
type unchangedArrayDouble struct{object objectstore.Object;calls int;changed bool}
func(c *unchangedArrayDouble)resolve(context.Context,auth.Identity,VerifiedNegativeUnchangedSource,NegativeSourceArraySpec)(objectstore.Object,error){c.calls++;o:=c.object;if c.changed&&c.calls>1{o.VersionID="changed"};return o,nil}
func TestUnchangedArraysHDRVersionsAndSession(t *testing.T){
 params,source,_,_:=negativeDraftFixture(t);spec,object,pixels:=perspectiveArrayFixture(t,3,[]float32{-.25,1.5,.4})
 image,err:=review.ParseWorkingImage(source);if err!=nil{t.Fatal(err)}
 image.Canvas.Width=1;image.Canvas.Height=1;image.Alpha=nil;image.Pixels.ID=spec.Ref.ArtifactID;image.Pixels.SHA256=spec.Ref.SHA256
 source,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(source)
 params.Base.Source.ArtifactID=uuid.New();params.Base.Source.SHA256=hex.EncodeToString(sum[:]);params.Base.CanvasHash,err=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 frozen,_,err:=params.Compile();if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 v:=VerifiedNegativeUnchangedSource{step:PreparedNegativeReplacementStep{intent:VerifiedOpticalFrozenIntent{raw:string(frozen),locator:negativeFrozenIntentLocator{Operation:"optical.negative"},who:who}},binding:params.Base,locator:negativeSourceLocator{ArtifactID:params.Base.Source.ArtifactID,SHA256:params.Base.Source.SHA256,Bucket:"photos",Key:"source",Version:"original",Bytes:int64(len(source)),Media:"application/json"},raw:string(source)}
 for _,mode:=range []string{"valid","version_changed","truncated","close_error","open_error_with_reader","nil_reader","foreign_session"}{t.Run(mode,func(t *testing.T){
  catalog:=&unchangedArrayDouble{object:object,changed:mode=="version_changed"};reader:=&negativeArrayReadDouble{raw:pixels,mode:mode};actor:=who;if mode=="foreign_session"{actor.SessionID=uuid.New()}
  proof,err:=loadNegativeUnchangedArrays(context.Background(),catalog,reader,actor,v)
  if mode=="valid"{if err!=nil||len(proof.objects)!=1||proof.objects[0]!=object||catalog.calls!=3{t.Fatal("unchanged HDR proof",err)}
   mapped:=PreparedMappedReplacementStep{parents:PreparedReplacementParentSources{step:v.step}}
   if _,err:=mapped.WithUnchangedSources(context.Background(),nil);err==nil{t.Fatal("missing unchanged source accepted")}
   attached,err:=mapped.WithUnchangedSources(context.Background(),[]VerifiedNegativeUnchangedArrays{proof});if err!=nil{t.Fatal("attach complete unchanged source",err)}
   original,err:=attached.sourceDocument(context.Background(),v.binding.Source);if err!=nil||string(original)!=string(source){t.Fatal("unchanged compile source",err)}
   if _,err:=mapped.WithUnchangedSources(context.Background(),[]VerifiedNegativeUnchangedArrays{proof,proof});err==nil{t.Fatal("duplicate unchanged source accepted")}
   proof.objects[0].VersionID="changed"
   if attached.unchanged[0].objects[0].VersionID!=object.VersionID{t.Fatal("caller mutation changed attached source")}
}else if err==nil||len(proof.objects)!=0{t.Fatal("invalid unchanged arrays accepted",mode)}
  if mode=="foreign_session"&&(reader.opens!=0||catalog.calls!=0){t.Fatal("foreign session reached IO")}
  if reader.opens>0&&mode!="nil_reader"&&reader.opens!=reader.closes{t.Fatal("reader leaked")}
 })}
 v.binding.CanvasHash="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 if _,err:=unchangedSourceArraySpecs(v);err==nil{t.Fatal("changed frozen canvas accepted")}
}
