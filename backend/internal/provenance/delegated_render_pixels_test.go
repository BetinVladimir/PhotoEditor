package provenance

import (
 "bytes"
 "context"
 "encoding/binary"
 "encoding/json"
 "io"
 "math"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type delegatedRenderPixelsCatalogFunc func(context.Context,CanonicalSigningTask,
 v2input.Ref,review.WorkingImage)(objectstore.Object,error)
func(f delegatedRenderPixelsCatalogFunc)LookupDelegatedRenderPixels(ctx context.Context,
 task CanonicalSigningTask,ref v2input.Ref,image review.WorkingImage)(objectstore.Object,error){
 return f(ctx,task,ref,image)
}

func TestDelegatedRenderRGBVerifiesPhysicalHDRAndVersion(t *testing.T){
 for _,mode:=range []string{"valid_hdr","nan","changed_version","cancelled"}{
  t.Run(mode,func(t *testing.T){
   header:="{'descr': '<f4', 'fortran_order': False, 'shape': (1, 1, 3), }\n"
   raw:=[]byte{0x93,'N','U','M','P','Y',1,0,0,0}
   binary.LittleEndian.PutUint16(raw[8:],uint16(len(header)))
   raw=append(raw,[]byte(header)...)
   for _,v:=range []float32{2,.5,.1}{
    var encoded [4]byte
    binary.LittleEndian.PutUint32(encoded[:],math.Float32bits(v))
    raw=append(raw,encoded[:]...)
   }
   if mode=="nan"{binary.LittleEndian.PutUint32(raw[len(raw)-4:],
    math.Float32bits(float32(math.NaN())))}
   sha:=bodyDigest(raw)
   image:=review.WorkingImage{DType:"float32",Canvas:review.Canvas{Width:1,Height:1},
    Pixels:review.ArtifactRef{ID:uuid.New(),SHA256:sha}}
   evidence:=SigningRenderPixelEvidence{result:v2draft.ImageResultDocument{
    WorkingImage:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}},image:image}
   object:=objectstore.Object{Key:"rgb",VersionID:"v1",SHA256:sha,
    Bytes:int64(len(raw)),ContentType:"application/x-npy"}
   calls:=0
   catalog:=delegatedRenderPixelsCatalogFunc(func(context.Context,CanonicalSigningTask,
    v2input.Ref,review.WorkingImage)(objectstore.Object,error){
    calls++;value:=object
    if mode=="changed_version"&&calls>1{value.VersionID="v2"}
    return value,nil
   })
   objects:=signingDocumentObjectsFunc(func(context.Context,objectstore.Object)(io.ReadCloser,error){
    return io.NopCloser(bytes.NewReader(raw)),nil
   })
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   if mode=="cancelled"{cancel()}
   task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"render.compose",Version:"1",
    Params:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},
    InputDigest:strings.Repeat("b",64),ResultDigest:strings.Repeat("c",64),
    Inputs:json.RawMessage(`[]`),Outputs:json.RawMessage(`[]`),Metrics:json.RawMessage(`{}`)}
   err:=(BoundDelegatedRenderPixelsVerifier{Catalog:catalog,Objects:objects}).Verify(
    ctx,task,evidence)
   if mode=="valid_hdr"{if err!=nil||calls!=2{t.Fatal("HDR render refused",err)}}
   if mode!="valid_hdr"&&err==nil{t.Fatal("invalid render RGB accepted",mode)}
   if mode=="cancelled"&&calls!=0{t.Fatal("cancelled read reached catalog")}
  })
 }
}
