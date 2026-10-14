package provenance

import (
 "bytes"
 "context"
 "encoding/binary"
 "encoding/json"
 "io"
 "math"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2draft"
)

type signingNegativePixelsCatalogFunc func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,review.WorkingImage)(objectstore.Object,error)
func(f signingNegativePixelsCatalogFunc)LookupSigningNegativePixels(c context.Context,a auth.Identity,r ReadySigningExport,t CanonicalSigningTask,p v2input.Ref,i review.WorkingImage)(objectstore.Object,error){return f(c,a,r,t,p,i)}

func TestNegativeRGBPhysicalBytesPreserveHDRAndRejectCorruption(t *testing.T){
 for _,mode:=range []string{"valid_hdr","nan","wrong_shape","truncated","hash","version","cancelled"}{
  t.Run(mode,func(t *testing.T){
   header:="{'descr': '<f4', 'fortran_order': False, 'shape': (1, 1, 3), }\n"
   if mode=="wrong_shape"{header="{'descr': '<f4', 'fortran_order': False, 'shape': (1, 3), }\n"}
   raw:=[]byte{0x93,'N','U','M','P','Y',1,0,0,0}
   binary.LittleEndian.PutUint16(raw[8:],uint16(len(header)));raw=append(raw,[]byte(header)...)
   for _,value:=range []float32{2,.5,.1}{var encoded [4]byte;binary.LittleEndian.PutUint32(encoded[:],math.Float32bits(value));raw=append(raw,encoded[:]...)}
   if mode=="nan"{binary.LittleEndian.PutUint32(raw[len(raw)-4:],math.Float32bits(float32(math.NaN())))}
   sha:=bodyDigest(raw)
   pin:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
   task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1,Operation:"optical.negative",Version:"1",Params:pin,InputDigest:sha,ResultDigest:sha,Inputs:json.RawMessage(`[]`),Outputs:json.RawMessage(`[]`),Metrics:json.RawMessage(`{}`)}
   taskSHA,_:=canonical.Hash(task)
   image:=review.WorkingImage{DType:"float32",Canvas:review.Canvas{Width:1,Height:1},Pixels:review.ArtifactRef{ID:uuid.New(),SHA256:sha}}
   evidence:=SigningNegativePixelEvidence{taskDigest:taskSHA,result:v2draft.ImageResultDocument{WorkingImage:pin},image:image}
   object:=objectstore.Object{Key:"rgb",VersionID:"v1",SHA256:sha,Bytes:int64(len(raw)),ContentType:"application/x-npy"}
   calls,opens:=0,0
   catalog:=signingNegativePixelsCatalogFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask,v2input.Ref,review.WorkingImage)(objectstore.Object,error){calls++;value:=object;if mode=="version"&&calls>1{value.VersionID="v2"};return value,nil})
   objects:=signingDocumentObjectsFunc(func(context.Context,objectstore.Object)(io.ReadCloser,error){opens++;body:=append([]byte(nil),raw...);if mode=="hash"{body[len(body)-1]^=1};if mode=="truncated"{body=body[:len(body)-1]};return io.NopCloser(bytes.NewReader(body)),nil})
   ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
   err:=(BoundSigningNegativePixelsVerifier{Catalog:catalog,Objects:objects}).Verify(ctx,auth.Identity{},ReadySigningExport{},task,evidence)
   if mode=="valid_hdr"{if err!=nil||calls!=2||opens!=1{t.Fatal("HDR pixels refused",err)};return}
   if err==nil{t.Fatal("corrupt RGB accepted",mode)}
   if mode=="cancelled"&&(calls!=0||opens!=0){t.Fatal("cancelled verification performed IO")}
  })
 }
}
