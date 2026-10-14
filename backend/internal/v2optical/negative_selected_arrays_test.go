package v2optical

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
)
type negativeSelectedArrayDouble struct{object objectstore.Object;calls int;mode string}
func(c *negativeSelectedArrayDouble)resolve(_ context.Context,_ auth.Identity,v VerifiedNegativeSelection,_ NegativeSourceArraySpec)(objectstore.Object,error){
 c.calls++;if c.mode=="mutate_callback" {v.source.Source.SHA256="changed"}
 o:=c.object;if c.mode=="version_changed"&&c.calls>1 {o.VersionID="changed"};return o,nil
}
func TestNegativeSelectedArraysRequireOriginalPixelsAndStableMetadata(t *testing.T){
 _,source,_,_:=negativeDraftFixture(t)
 spec,object,pixel:=perspectiveArrayFixture(t,3,[]float32{-0.25,1.5,0.4})
 image,err:=review.ParseWorkingImage(source);if err!=nil {t.Fatal(err)}
 image.Canvas.Width=1;image.Canvas.Height=1;image.Alpha=nil;image.Pixels.ID=spec.Ref.ArtifactID;image.Pixels.SHA256=object.SHA256
 source,err=canonical.Marshal(image);if err!=nil {t.Fatal(err)}
 v:=VerifiedNegativeSelection{source:negativeSelectedSource{SchemaVersion:1,Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),Task:uuid.New(),Run:uuid.New(),Hash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Route:"base",Source:&negativeSourceLocator{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},raw:string(source)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 for _,mode:=range []string{"valid","mutate_callback","version_changed","truncated","close_error","open_error_with_reader","nil_reader"} {
  t.Run(mode,func(t *testing.T){catalog:=&negativeSelectedArrayDouble{object:object,mode:mode};reader:=&negativeArrayReadDouble{raw:pixel,mode:mode}
   proof,err:=loadNegativeSelectedArrays(context.Background(),catalog,reader,who,v)
   if mode=="valid"||mode=="mutate_callback" {if err!=nil||len(proof.objects)!=1||proof.objects[0]!=object||proof.selection.source.Source.SHA256!=v.source.Source.SHA256||catalog.calls!=3 {t.Fatal("source proof",err)}} else if err==nil||len(proof.objects)!=0 {t.Fatal("invalid arrays accepted",mode)}
   if reader.opens>0&&mode!="nil_reader"&&reader.opens!=reader.closes {t.Fatal("reader leaked")}
  })
 }
 var chain map[string]any
 if err=json.Unmarshal(image.TransformChain,&chain);err!=nil {t.Fatal(err)}
 chain["steps"]=[]any{map[string]any{"kind":"matrix","inverse_field":map[string]any{"artifact_id":uuid.New().String(),"sha256":object.SHA256}}}
 image.TransformChain,_=json.Marshal(chain);raw,_:=canonical.Marshal(image);v.raw=string(raw)
 if _,err:=selectedNegativeArraySpecs(v);err==nil {t.Fatal("hidden inverse field accepted")}
}
