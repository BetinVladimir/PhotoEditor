package v2optical

import (
 "bytes"
 "context"
 "errors"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
)

type negativeArrayCatalogDouble struct { object objectstore.Object; mode string; calls int }
func(c *negativeArrayCatalogDouble)ResolveNegativeSourceArray(_ context.Context,_ auth.Identity,_,_,_ uuid.UUID,_ NegativeParams,source []byte,_ NegativeSourceArraySpec)(objectstore.Object,error) {
 c.calls++
 if c.mode=="catalog_error" { return objectstore.Object{},errors.New("catalog unavailable") }
 if c.mode=="mutate_callback" { source[0]='[' }
 object:=c.object
 if c.mode=="version_changed"&&c.calls>1 { object.VersionID="changed" }
 return object,nil
}
type negativeArrayReadDouble struct { raw []byte; mode string; opens,closes int }
type negativeArrayReadCloser struct { io.Reader; owner *negativeArrayReadDouble }
func(r *negativeArrayReadCloser)Close()error { r.owner.closes++;if r.owner.mode=="close_error" { return errors.New("close failed") };return nil }
func(r *negativeArrayReadDouble)Open(_ context.Context,_ objectstore.Object)(io.ReadCloser,error) {
 r.opens++
 raw:=append([]byte(nil),r.raw...)
 if r.mode=="truncated" { raw=raw[:len(raw)-1] }
 reader:=&negativeArrayReadCloser{Reader:bytes.NewReader(raw),owner:r}
 if r.mode=="open_error_with_reader" { return reader,errors.New("open failed") }
 if r.mode=="nil_reader" { return nil,nil }
 return reader,nil
}
func TestNegativePhysicalReceiptRequiresOriginalBytesAndStableLocators(t *testing.T) {
 params,source,_,_:=negativeDraftFixture(t)
 image,err:=review.ParseWorkingImage(source);if err!=nil { t.Fatal(err) }
 spec,object,raw:=perspectiveArrayFixture(t,3,[]float32{-.1,.5,2})
 image.Canvas.Width=1;image.Canvas.Height=1;image.Alpha=nil
 image.Pixels=review.ArtifactRef{ID:spec.Ref.ArtifactID,SHA256:spec.Ref.SHA256}
 source,err=canonical.Marshal(image);if err!=nil { t.Fatal(err) }
 params.Base.Source.SHA256,err=canonical.Hash(image);if err!=nil { t.Fatal(err) }
 params.Base.CanvasHash,err=canonical.Hash(image.Canvas);if err!=nil { t.Fatal(err) }
 for _,mode:=range []string{"valid","mutate_callback","catalog_error","version_changed","truncated","close_error","open_error_with_reader","nil_reader"} {
  t.Run(mode,func(t *testing.T){
   catalog:=&negativeArrayCatalogDouble{object:object,mode:mode};reader:=&negativeArrayReadDouble{raw:raw,mode:mode}
   who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,project,snapshot:=uuid.New(),uuid.New(),uuid.New()
   proof,err:=readNegativeSourceProof(context.Background(),catalog,reader,who,tenant,project,snapshot,params,source)
   if mode=="valid"||mode=="mutate_callback" {
    if err!=nil||proof==nil||len(proof.objects)!=1||proof.objects[0]!=object||proof.source!=string(source)||proof.who.UserID!=who.UserID||catalog.calls!=3 { t.Fatal("physical proof not bound",err) }
   }else if err==nil||proof!=nil { t.Fatal("invalid physical evidence accepted",mode) }
   if reader.opens>0&&mode!="nil_reader"&&reader.closes!=reader.opens { t.Fatal("physical reader leaked",reader.opens,reader.closes) }
  })
 }
}
