package delivery

import (
 "bytes"
 "context"
 "encoding/binary"
 "fmt"
 "io"
 "strings"
 "testing"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type watermarkArrayReader struct{bodies map[string][]byte;opens int}
func(r *watermarkArrayReader)Open(_ context.Context,o objectstore.Object)(io.ReadCloser,error){r.opens++;return io.NopCloser(bytes.NewReader(r.bodies[o.Key])),nil}
func watermarkArrayNPY(alpha,uint16 bool)[]byte{
 dtype,shape,size:="<f4","(50, 100, 3)",100*50*12
 if alpha{shape="(50, 100)";size=100*50*4};if uint16{dtype="<u2";size=100*50*2}
 header:=fmt.Sprintf("{'descr': '%s', 'fortran_order': False, 'shape': %s, }",dtype,shape)
 header+=strings.Repeat(" ",(16-(10+len(header)+1)%16)%16)+"\n"
 raw:=append([]byte("\x93NUMPY\x01\x00"),byte(len(header)),byte(len(header)>>8));raw=append(raw,[]byte(header)...);return append(raw,make([]byte,size)...)
}
func TestWatermarkSourceArrayPhysicalVerification(t *testing.T){
 for _,mode:=range []string{"float32","uint16_alpha","hash_corrupt","nan_rgb","alpha_out_of_range","bad_second_descriptor"}{t.Run(mode,func(t *testing.T){
  _,_,e:=watermarkPinFixture(t)
  rgb:=watermarkArrayNPY(false,false);alpha:=watermarkArrayNPY(true,mode=="uint16_alpha")
  if mode=="nan_rgb"{binary.LittleEndian.PutUint32(rgb[len(rgb)-4:],0x7fc00000)}
  if mode=="alpha_out_of_range"{binary.LittleEndian.PutUint32(alpha[len(alpha)-4:],0x40000000)}
  image,err:=review.ParseWorkingImage(e.Manifest);if err!=nil{t.Fatal(err)}
  image.Pixels.SHA256=digest(rgb);image.Alpha.SHA256=digest(alpha)
  e.Manifest,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)};e.Source.Ref.SHA256=digest(e.Manifest)
  e.Arrays[0].Ref=v2input.Ref{ArtifactID:image.Pixels.ID,SHA256:image.Pixels.SHA256}
  e.Arrays[1].Ref=v2input.Ref{ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256}
  for n,body:=range [][]byte{rgb,alpha}{e.Arrays[n].Object=objectstore.Object{Key:fmt.Sprint(n),VersionID:"v1",ContentType:"application/octet-stream",SHA256:digest(body),Bytes:int64(len(body))}}
  reader:=&watermarkArrayReader{bodies:map[string][]byte{"0":rgb,"1":alpha}}
  if mode=="hash_corrupt"{corrupt:=append([]byte(nil),rgb...);corrupt[len(corrupt)-1]^=1;reader.bodies["0"]=corrupt}
  if mode=="bad_second_descriptor"{e.Arrays[1].Object.VersionID="null"}
  err=VerifyWatermarkSourceArrays(context.Background(),reader,e)
  valid:=mode=="float32"||mode=="uint16_alpha"
  if (err==nil)!=valid{t.Fatal(mode,err)}
  if valid&&reader.opens!=2{t.Fatal("incomplete binary verification")}
  if mode=="bad_second_descriptor"&&reader.opens!=0{t.Fatal("IO before complete preflight")}
 })}
}
